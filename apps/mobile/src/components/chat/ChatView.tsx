import type { ConversationReference, Message, ReasoningEffort } from '@openchat/api-client'
import { useQueryClient } from '@tanstack/react-query'
import { ArrowDown, Menu, NotebookPen, SquarePen, Workflow } from 'lucide-react-native'
import { useCallback, useEffect, useEffectEvent, useMemo, useRef, useState, type ComponentRef } from 'react'
import { useTranslation } from 'react-i18next'
import { BlurTargetView } from 'expo-blur'
import { FlatList, Pressable, StyleSheet, Text, useWindowDimensions, View, type NativeScrollEvent, type NativeSyntheticEvent } from 'react-native'
import { KeyboardAvoidingView, useReanimatedKeyboardAnimation } from 'react-native-keyboard-controller'
import Animated, { useAnimatedStyle } from 'react-native-reanimated'
import { useSafeAreaInsets } from 'react-native-safe-area-context'
import Svg, { Defs, LinearGradient, Rect, Stop } from 'react-native-svg'

import { parseQuestions, type AskAnswer } from '@openchat/chat-core/ask-user'
import { greetingLines, pickGreeting } from '@openchat/chat-core/greeting'
import { gapLabel, timeGaps } from '@openchat/chat-core/time-gap'
import { quoteMessage } from '@openchat/chat-core/document-selection'
import { defaultModelId } from '@openchat/chat-core/model-capacity'
import { composerMode, reviseDraft } from '@openchat/chat-core/revise'
import { isRunning } from '@openchat/chat-core/run-state'
import { returnableFiles, startWroteNothing } from '@openchat/chat-core/start-failure'
import { acceptsOf, TEXT_TYPE } from '@openchat/chat-core/attachments'
import { buildTurns, pendingQuestion, regenerable, type AssistantTurn, type Turn } from '@openchat/chat-core/transcript'

import { useAttachments } from '@/hooks/attachments'
import { useCompaction, useUndoCompaction } from '@/hooks/compaction'
import { useConversationDocuments } from '@/hooks/documents'
import { useSubagents } from '@/hooks/subagents'
import { useSubagentPanel } from '@/lib/subagent-panel'
import { useConversation, useCreateConversation, useDeleteConversation } from '@/hooks/conversations'
import { useConversationEvents } from '@/hooks/events'
import { useMessages } from '@/hooks/messages'
import { freshModels, useChatFeatures, useChatModels, useUpdateConversationSettings } from '@/hooks/models'
import { useAnswerQuestion, useCancelRun, useEditMessage, useRegenerate, useSendMessage } from '@/hooks/send'
import { useComposerPrefs } from '@/lib/composer-prefs'
import { useDocumentQuote, useDocumentScreen } from '@/lib/documents'
import { useShared } from '@/lib/shared'
import { useUser } from '@/lib/session'
import { useChatStore } from '@/lib/store'
import { toast } from '@/lib/toast'
import { HeaderIcon, HeaderPill } from '@/components/HeaderPill'
import { useColors } from '@/theme'
import { AskUserDock } from './AskUserDock'
import { AttachMenu } from './AttachMenu'
import { EffortSlider } from './EffortSlider'
import { KeepInPlace } from './keep-in-place'
import { ModelPicker } from './ModelPicker'
import { AttachmentTray } from './AttachmentTray'
import { Composer } from './Composer'
import { Divider } from './Divider'
import { DocumentScreen } from './DocumentScreen'
import { SubagentReportLines } from './SubagentLines'
import { SubagentPanel } from './SubagentPanel'
import { QuoteCard } from './QuoteCard'
import { Sheet, SheetOption } from '@/components/Sheet'
import { ChoicesContext, type ChoicesActions } from './genui/GenUIBlock'
import { ImageSource, proxiedImages } from './genui/pictures'
import { PendingReply, RefusedReply, Reply } from './Reply'
import { UserBubble } from './UserBubble'

interface Props {
  /** The conversation shown; none on the new-chat screen. */
  conversationId?: string
  /** The first message of a new chat made a conversation, which this view shows from now on. */
  onCreated: (conversationId: string) => void
  /** That conversation was removed, the server having refused the message before writing anything. */
  onRemoved: (conversationId: string) => void
  /** The address should name this conversation now (the server took the first message). */
  onSettled: (conversationId: string) => void
  /** The conversation is gone (deleted elsewhere): a new chat. */
  onGone: () => void
  onOpenDrawer: () => void
  onNewChat: () => void
}

type Row =
  | { key: string; kind: 'turn'; turn: Turn }
  | { key: string; kind: 'divider'; label: string; compaction?: boolean }
  | { key: string; kind: 'outbox' }
  | { key: string; kind: 'pending' }

/** Distance from the bottom, in px, still counted as being at the end. */
const BOTTOM_SLACK = 48

const turnModel = (turn: AssistantTurn) => turn.live?.run.model ?? turn.responses.find((m) => m.model)?.model

/**
 * One conversation, or the new-chat screen: the web's ChatView on a phone. The
 * transcript is an inverted list - its end is where it opens, and a growing
 * answer pushes nothing the reader is looking at - and the composer (or the
 * model's question) sits under it, above the keyboard.
 */
export function ChatView({ conversationId, onCreated, onRemoved, onSettled, onGone, onOpenDrawer, onNewChat }: Props) {
  const { t, i18n } = useTranslation()
  const colors = useColors()
  const insets = useSafeAreaInsets()
  const queryClient = useQueryClient()
  const user = useUser()
  const userId = user?.id ?? ''

  const [input, setInput] = useState('')
  const [showJump, setShowJump] = useState(false)
  const list = useRef<FlatList<Row>>(null)

  const conversation = useConversation(conversationId)
  const messages = useMessages(conversationId)
  const models = useChatModels()
  const features = useChatFeatures()
  const createConversation = useCreateConversation()
  const deleteConversation = useDeleteConversation()
  const sendMessage = useSendMessage(conversationId ?? '')
  const cancelRun = useCancelRun(conversationId ?? '')
  const regenerate = useRegenerate(conversationId ?? '')
  const editMessage = useEditMessage(conversationId ?? '')
  const answerQuestion = useAnswerQuestion(conversationId ?? '')
  const summary = useCompaction(conversationId)
  const undoCompaction = useUndoCompaction(conversationId ?? '')
  const updateSettings = useUpdateConversationSettings()

  useConversationEvents(conversationId, onGone)

  const live = useChatStore((s) => (conversationId ? s.live[conversationId] : undefined))
  const outgoing = useChatStore((s) => s.outbox[conversationId ?? ''])
  const post = useChatStore((s) => s.post)
  const unpost = useChatStore((s) => s.unpost)
  const refusals = useChatStore((s) => s.refusals)
  const replacing = useChatStore((s) => s.replacing[conversationId ?? ''])

  const superseded =
    isRunning(live) || sendMessage.isPending || regenerate.isPending || editMessage.isPending || Boolean(outgoing) || Boolean(replacing)
  const working = superseded || answerQuestion.isPending

  // The model, effort and web access: the conversation's own, else what was
  // picked here, else what was last picked anywhere, else the first offered
  // (web ChatView has the full reasoning).
  const saved = useComposerPrefs((s) => s.byUser[userId]) ?? {}
  const savePrefs = useComposerPrefs((s) => s.save)
  const available = useMemo(() => models.data ?? [], [models.data])
  const modelById = useMemo(() => new Map(available.map((m) => [m.id, m])), [available])
  const [draftModel, setDraftModel] = useState<string>()
  const offered = (id: string | null | undefined) => (id && (models.data === undefined || modelById.has(id)) ? id : undefined)
  const modelId = offered(conversation.data?.model) ?? offered(draftModel) ?? defaultModelId(available, saved.model)

  const pickModel = (id: string) => {
    setDraftModel(id)
    savePrefs(userId, { model: id })
    if (conversationId) updateSettings.mutate({ conversationId, model: id })
  }
  const [draftEffort, setDraftEffort] = useState<ReasoningEffort>()
  const effort = conversation.data?.reasoningEffort ?? draftEffort ?? saved.effort ?? 'auto'
  const thinkingLevels = useMemo(() => modelById.get(modelId)?.reasoningEfforts ?? [], [modelById, modelId])
  const canThink = thinkingLevels.length > 0
  const pickEffort = (next: ReasoningEffort) => {
    setDraftEffort(next)
    savePrefs(userId, { effort: next })
    if (conversationId) updateSettings.mutate({ conversationId, reasoningEffort: next })
  }
  const effortLevels = useMemo<ReasoningEffort[]>(() => ['auto', ...thinkingLevels], [thinkingLevels])
  const [choosingEffort, setChoosingEffort] = useState(false)
  const closeEffort = useCallback(() => setChoosingEffort(false), [])
  const page = useRef<ComponentRef<typeof View>>(null)
  // Clear of the gesture bar at rest; with the keyboard up the page is already
  // lifted by all of it, so the bar's room goes, following the keyboard.
  const keyboard = useReanimatedKeyboardAnimation()
  const [dockHeight, setDockHeight] = useState(0)
  const [headerHeight, setHeaderHeight] = useState(0)
  const window = useWindowDimensions()
  const resting = insets.bottom + 10
  const dockSpace = useAnimatedStyle(() => ({ paddingBottom: resting + (8 - resting) * keyboard.progress.value }))
  const [draftWebSearch, setDraftWebSearch] = useState<boolean>()
  const webSearch = conversation.data?.webSearch ?? draftWebSearch ?? saved.webSearch ?? true
  const canSearch = (features.data?.webSearch ?? false) && (modelById.get(modelId)?.toolCall ?? true)
  const toggleWebSearch = () => {
    const next = !webSearch
    setDraftWebSearch(next)
    savePrefs(userId, { webSearch: next })
    if (conversationId) updateSettings.mutate({ conversationId, webSearch: next })
  }
  // Files: only what the selected model takes; text files go to any model as words.
  const accepts = useMemo(() => acceptsOf(modelById.get(modelId)), [modelById, modelId])
  const attachments = useAttachments(accepts)
  // Other conversations the message refers to: not files, nothing goes up.
  const [references, setReferences] = useState<ConversationReference[]>([])
  const [attaching, setAttaching] = useState(false)
  // A writing block's passage quoted for this conversation's next message ("提出修改请求").
  const allQuotes = useDocumentQuote((s) => s.quotes)
  const quotes = useMemo(() => allQuotes.filter((q) => q.conversationId === conversationId), [allQuotes, conversationId])
  const setQuote = useDocumentQuote((s) => s.set)
  const [listing, setListing] = useState(false)
  const openDocument = useDocumentScreen((s) => s.open)
  const documents = useConversationDocuments(conversationId)
  const subagents = useSubagents(conversationId)
  const openSubagents = useSubagentPanel((s) => s.open)

  const choice = () => ({
    model: modelId || undefined,
    reasoningEffort: canThink ? effort : undefined,
    webSearch: canSearch ? webSearch : undefined,
  })

  const turns = useMemo(
    () => buildTurns(messages.data?.items ?? [], live?.run ?? null, replacing, summary),
    [messages.data, live, replacing, summary],
  )
  const mode = useMemo(() => composerMode(turns), [turns])
  const revising = mode.kind === 'revise' ? mode.message : null
  const last = useMemo(() => regenerable(turns), [turns])

  const askingCallId = useMemo(() => pendingQuestion(turns, superseded), [turns, superseded])
  const asking = useMemo(() => {
    if (!askingCallId) return null
    for (const turn of turns) {
      if (turn.kind !== 'assistant') continue
      for (const response of turn.responses) {
        for (const part of response.parts) {
          if (part.type === 'tool_call' && part.id === askingCallId) {
            const questions = parseQuestions(part.arguments)
            return questions ? { callId: askingCallId, questions } : null
          }
        }
      }
    }
    return null
  }, [askingCallId, turns])
  const [dismissedCallId, setDismissedCallId] = useState<string | null>(null)

  // Whether this view is still on screen: a send under way when the reader
  // opened another conversation must not touch the view that replaced it.
  const alive = useRef(true)
  useEffect(() => {
    alive.current = true
    return () => {
      alive.current = false
    }
  }, [])

  // Something another app shared waits for a new chat (components/ShareInbox):
  // its words in the box, its files in the tray, once the models are known.
  const shared = useShared((s) => s.shared)
  const takeShared = useShared((s) => s.take)
  const takeShare = useEffectEvent(() => {
    const taken = takeShared()
    if (!taken) return
    if (taken.text) setInput((current) => (current ? `${current}\n${taken.text}` : taken.text))
    if (taken.files.length > 0) void attachments.add(taken.files)
  })
  const modelsKnown = Boolean(models.data)
  useEffect(() => {
    if (conversationId || !shared || !modelsKnown) return
    // A tick later: the box is filled after this render, not inside it.
    const timer = setTimeout(takeShare, 0)
    return () => clearTimeout(timer)
  }, [conversationId, shared, modelsKnown])

  // The message of a first send was shown under the empty key until the view
  // took the conversation as its own; now it is under the conversation's.
  useEffect(() => {
    if (conversationId) unpost('')
  }, [conversationId, unpost])

  // The refused message the composer was filled with, so that a person who
  // empties the box is not given its words back (chat-core/revise.ts).
  const filledFor = useRef<string | null>(null)
  const fillRevision = useEffectEvent(() => {
    const next = reviseDraft(mode, input, filledFor.current)
    filledFor.current = next.handled
    if (next.fill !== null) setInput(next.fill)
  })
  useEffect(() => {
    fillRevision()
  }, [revising?.id])

  // The model at its cap: said, and nothing sent. Asked afresh first - the cap may have been raised.
  const capacityRefused = useCallback(async () => {
    if (!modelById.get(modelId)?.atCapacity) return false
    const fresh = await freshModels(queryClient).catch(() => undefined)
    if (!fresh?.find((m) => m.id === modelId)?.atCapacity) return false
    toast.error(t('chat.send-error.model_capacity_reached'))
    return true
  }, [modelById, modelId, queryClient, t])

  const toEnd = () => list.current?.scrollToOffset({ offset: 0, animated: true })

  const handleSend = async () => {
    const typed = input.trim()
    if (!typed) return
    // A quoted passage goes in front (chat-core/document-selection), unless
    // this is the rewording of a refused message, which keeps its own words.
    const quoted = quotes.length > 0 && !revising ? quotes : null
    const content = quoted ? quoteMessage(quoted, typed) : typed
    if (attachments.uploading) return toast.info(t('chat.file.wait'))
    if (attachments.failed) return toast.error(t('chat.file.failed'))
    const now = useChatStore.getState()
    if (working || createConversation.isPending || now.outbox[conversationId ?? ''] || (conversationId && isRunning(now.live[conversationId]))) return
    if (await capacityRefused()) return
    const attachmentIds = attachments.ids
    const referred = references
    const conversationIds = referred.map((r) => r.conversationId)
    // Back in the box when the send fails, unless others were chosen meanwhile.
    const referBack = () => setReferences((current) => (current.length > 0 ? current : referred))
    setInput('')
    setReferences([])
    // Out of the box at once, as the text is: the message shows them now.
    const taken = attachments.take()
    const item = {
      content,
      attachments: taken
        .filter((file) => file.id)
        .map((file) => ({
          id: file.id!,
          filename: file.name,
          contentType: file.kind === 'image' ? 'image/*' : file.kind === 'pdf' ? 'application/pdf' : TEXT_TYPE,
          size: file.size,
          url: file.preview ?? '',
        })),
      references: referred,
    }
    if (quoted) setQuote(null)
    const giveBack = () => {
      setInput((current) => current || typed)
      if (quoted) setQuote(quoted[0]!)
      attachments.restore(taken)
      referBack()
    }
    toEnd()

    if (conversationId && revising) {
      // The refused message is reworded, not followed: an edit of it, which
      // takes the files in the box with it.
      editMessage.mutate(
        { messageId: revising.id, content, attachments: item.attachments, references: referred, attachmentIds, conversationIds, ...choice() },
        { onSuccess: () => attachments.release(taken), onError: giveBack },
      )
      return
    }
    if (conversationId) {
      post(conversationId, item)
      sendMessage.mutate(
        { content, attachmentIds, conversationIds, ...choice() },
        {
          onSuccess: () => attachments.release(taken),
          onError: () => {
            unpost(conversationId)
            giveBack()
          },
        },
      )
      return
    }

    // No conversation yet: make one, take it as this view's own at once, and
    // send to it; the address follows once the server has answered.
    post('', item)
    let created
    try {
      created = await createConversation.mutateAsync({
        ...(modelId ? { model: modelId } : {}),
        ...(canThink && effort !== 'auto' ? { reasoningEffort: effort } : {}),
        ...(canSearch && !webSearch ? { webSearch: false } : {}),
      })
    } catch {
      unpost('', item)
      if (!alive.current) return
      giveBack()
      toast.error(t('chat.send-error.generic'))
      return
    }
    const madeId = created.id
    post(madeId, item)
    if (alive.current) onCreated(madeId)
    else unpost('', item)
    try {
      await sendMessage.mutateAsync({ conversationId: madeId, content, attachmentIds, conversationIds, ...choice() })
    } catch (error) {
      // Refused before anything was written: the conversation was made for
      // this message alone, so it goes, and this is the new chat again.
      const wroteNothing = startWroteNothing(error)
      if (wroteNothing) deleteConversation.mutate(madeId)
      if (!alive.current) return
      // Uploads the server says it cannot use would only be refused again.
      const usable = returnableFiles(error, taken)
      attachments.release(taken.filter((file) => !usable.includes(file)))
      attachments.restore(usable)
      referBack()
      setInput((current) => current || typed)
      if (wroteNothing) onRemoved(madeId)
      else onSettled(madeId)
      return
    }
    attachments.release(taken)
    if (alive.current) onSettled(madeId)
  }

  // A choice picked in the last answer (GenUI Choices): sent as the reader's
  // next message, leaving whatever is in the composer where it is.
  const handlePick = async (content: string) => {
    if (!conversationId || revising) return
    const now = useChatStore.getState()
    if (working || now.outbox[conversationId] || isRunning(now.live[conversationId])) return
    if (await capacityRefused()) return
    const item = { content, attachments: [] }
    toEnd()
    post(conversationId, item)
    sendMessage.mutate(
      { content, ...choice() },
      {
        onError: () => {
          unpost(conversationId, item)
          setInput((current) => current || content)
        },
      },
    )
  }

  // What was sent after each reply, for its Choices to mark the choice it was.
  const sentAfter = useMemo(() => {
    const byTurn = new Map<string, ChoicesActions>()
    let next: string | undefined = outgoing?.content
    for (let i = turns.length - 1; i >= 0; i--) {
      const turn = turns[i]
      if (!turn) continue
      if (turn.kind === 'user') next = turn.message.content
      else if (turn.kind === 'assistant') byTurn.set(turn.key, { next })
    }
    return byTurn
  }, [outgoing, turns])
  const canPick = !working && !outgoing && !revising
  const images = useMemo(() => (conversationId ? proxiedImages(conversationId) : () => null), [conversationId])

  const handleRegenerate = () => {
    if (!last) return
    toEnd()
    regenerate.mutate({ inputId: last.inputId, ...choice() })
  }
  const editQuestion = (message: Message, content: string) => {
    toEnd()
    editMessage.mutate({ messageId: message.id, content, attachments: message.attachments ?? [], references: message.references, ...choice() })
  }
  const handleAnswer = (callId: string, answers: AskAnswer[]) => {
    toEnd()
    answerQuestion.mutate({ callId, answers, ...choice() })
  }

  // "Today" and "yesterday" are from when the view opened.
  const [openedAt] = useState(() => Date.now())

  // The transcript, newest first for the inverted list: where the person picked
  // the conversation up again after a pause, a time above their message (as
  // the model is told it); where the context was compressed, a line.
  const rows = useMemo(() => {
    const out: Row[] = []
    const gaps = timeGaps(turns)
    for (const turn of turns) {
      const gap = gaps.get(turn.key)
      if (gap !== undefined) {
        const label = gapLabel(gap, Math.max(openedAt, gap), i18n.language, { today: t('chat.time-gap.today'), yesterday: t('chat.time-gap.yesterday') })
        out.push({ key: `${turn.key}:gap`, kind: 'divider', label })
      }
      out.push({ key: turn.key, kind: 'turn', turn })
      if (turn.after) out.push({ key: `${turn.key}:compaction`, kind: 'divider', label: t('chat.compaction.divider'), compaction: true })
    }
    if (replacing && !outgoing) out.push({ key: 'pending', kind: 'pending' })
    if (outgoing) out.push({ key: 'outbox', kind: 'outbox' }, { key: 'outbox-reply', kind: 'pending' })
    return out.reverse()
  }, [turns, replacing, outgoing, t, i18n.language, openedAt])

  const isEmpty = !outgoing && (!conversationId || (!messages.isPending && turns.length === 0))
  const runId = live?.run?.run.status === 'running' ? live.run.run.id : undefined

  const renderRow = ({ item, index }: { item: Row; index: number }) => {
    if (item.kind === 'divider') {
      // Taking the compression back is not offered under a run: it would change
      // the context in the middle of the run's tool loop.
      const undo =
        item.compaction && !working && !outgoing
          ? { label: t('chat.compaction.undo'), onPress: () => undoCompaction.mutate(), busy: undoCompaction.isPending }
          : undefined
      return <Divider label={item.label} action={undo} />
    }
    if (item.kind === 'pending') return <PendingReply modelId={modelId} model={modelById.get(modelId)} />
    if (item.kind === 'outbox') return outgoing ? <UserBubble message={outgoing} /> : null
    const turn = item.turn
    if (turn.kind === 'user') {
      const refused = turn.message.status === 'blocked'
      return (
        <UserBubble
          message={turn.message}
          refused={refused}
          onEdit={!working && !outgoing && turn.message.id !== revising?.id ? (content) => editQuestion(turn.message, content) : undefined}
          // Once the keyboard is up, the field is brought to the middle of what is left.
          onEditStart={() => setTimeout(() => list.current?.scrollToIndex({ index, viewPosition: 0.5, animated: true }), 350)}
        />
      )
    }
    // Sub-agents' reports, delivered while the conversation was idle: a line
    // each, and the reply they started below.
    if (turn.kind === 'reports') return <SubagentReportLines conversationId={turn.message.conversationId} notices={turn.notices} />
    if (turn.kind === 'refusal') return <RefusedReply modelId={modelId} model={modelById.get(modelId)} refusal={refusals[turn.message.id]} />
    const id = turnModel(turn) ?? modelId
    const choices: ChoicesActions =
      turn === last?.answer && canPick
        ? { next: sentAfter.get(turn.key)?.next, pick: (message) => void handlePick(message) }
        : (sentAfter.get(turn.key) ?? NO_CHOICES)
    return (
      <ChoicesContext.Provider value={choices}>
        <Reply
          turn={turn}
          modelId={id}
          model={modelById.get(id)}
          askingCallId={askingCallId}
          onOpenQuestion={() => setDismissedCallId(null)}
          onRegenerate={turn === last?.answer && !working && !outgoing ? handleRegenerate : undefined}
        />
      </ChoicesContext.Provider>
    )
  }

  const offset = useRef(0)
  const onScroll = (event: NativeSyntheticEvent<NativeScrollEvent>) => {
    offset.current = event.nativeEvent.contentOffset.y
    setShowJump(event.nativeEvent.contentOffset.y > BOTTOM_SLACK)
  }
  // A part of an answer opened or closed: the list moves by its change in
  // height, so the line the reader tapped stays under their finger. The list
  // learns its new length before the part reports its new height, but has not
  // laid it out yet: a scroll now would stop at the old end, so it waits a frame.
  const keepInPlace = useCallback((delta: number) => {
    if (Math.abs(delta) < 1) return
    requestAnimationFrame(() => {
      offset.current = Math.max(0, offset.current + delta)
      list.current?.scrollToOffset({ offset: offset.current, animated: false })
    })
  }, [])

  const [greeting] = useState(() => pickGreeting(greetingLines(new Date(), Boolean(user?.name)), null))
  const showQuestion = asking && dismissedCallId !== asking.callId

  return (
    <View style={[styles.page, { backgroundColor: colors.bg }]}>
    <KeyboardAvoidingView behavior="padding" style={styles.page}>
      {/* What the effort slider blurs: on Android a blur sees only the view it is given. */}
      <BlurTargetView ref={page} style={[styles.page, { backgroundColor: colors.bg }]}>
      {/* Its one child, always the same view: expo-blur passes the target's children on to a view of
          its own, and a direct child appearing or being flattened away put the two out of step (a crash). */}
      <View collapsable={false} style={styles.page}>
      {/* Over the transcript, which runs on under it as it does under the composer. */}
      <View style={[styles.header, { paddingTop: insets.top + 4 }]} pointerEvents="box-none" onLayout={(event) => setHeaderHeight(event.nativeEvent.layout.height)}>
        {/* A veil of the page's colour, so the text going under the header shows through faintly and
            never runs into the clock or the pills. It thins out below the pills rather than stopping at
            a line. */}
        <Svg pointerEvents="none" style={styles.veil} width={window.width} height={headerHeight + VEIL_FADE}>
          <Defs>
            <LinearGradient id="veil" x1="0" y1="0" x2="0" y2="1">
              <Stop offset="0" stopColor={colors.bg} stopOpacity={0.85} />
              <Stop offset={headerHeight ? Math.max(0, (headerHeight - 16) / (headerHeight + VEIL_FADE)) : 0.6} stopColor={colors.bg} stopOpacity={0.85} />
              <Stop offset="1" stopColor={colors.bg} stopOpacity={0} />
            </LinearGradient>
          </Defs>
          <Rect x="0" y="0" width={window.width} height={headerHeight + VEIL_FADE} fill="url(#veil)" />
        </Svg>
        <HeaderPill>
          <HeaderIcon onPress={onOpenDrawer} label={t('chat.sidebar.open')}>
            <Menu size={20} color={colors.text} />
          </HeaderIcon>
        </HeaderPill>
        <ModelPicker models={available} modelId={modelId} onPick={pickModel} />
        <View style={styles.fill} />
        {/* What the conversation has, and a new one, share one pill. */}
        <HeaderPill>
          {subagents.data?.items.length ? (
            <HeaderIcon onPress={() => conversationId && openSubagents(conversationId)} label={t('subagents.open')}>
              <Workflow size={19} color={colors.text} />
            </HeaderIcon>
          ) : null}
          {documents.data?.length ? (
            <HeaderIcon onPress={() => setListing(true)} label={t('document.menu.open', { count: documents.data.length })}>
              <NotebookPen size={19} color={colors.text} />
            </HeaderIcon>
          ) : null}
          <HeaderIcon onPress={onNewChat} label={t('chat.search.new-chat')}>
            <SquarePen size={20} color={colors.text} />
          </HeaderIcon>
        </HeaderPill>
      </View>

      {isEmpty ? (
        <View style={styles.welcome}>
          <Text style={[styles.greeting, { color: colors.text }]}>{t(greeting.key, { name: user?.name ?? '' })}</Text>
        </View>
      ) : (
        <KeepInPlace.Provider value={keepInPlace}>
        <ImageSource.Provider value={images}>
          {/* Runs on under the composer, which floats over its foot: what is
              below the box and around it is the transcript, not a bar. */}
          <View style={[styles.fill, { marginTop: -headerHeight, marginBottom: -dockHeight }]}>
            <FlatList
              ref={list}
              data={rows}
              inverted
              keyExtractor={(row) => row.key}
              renderItem={renderRow}
              // Inverted: the top padding is the end of the transcript, kept clear of the composer.
              contentContainerStyle={[styles.transcript, { paddingTop: 16 + dockHeight, paddingBottom: 16 + headerHeight }]}
              ItemSeparatorComponent={Gap}
              keyboardDismissMode="interactive"
              keyboardShouldPersistTaps="handled"
              onScroll={onScroll}
              scrollEventThrottle={64}
              // At the end, nothing is held: the inverted list's end is offset 0,
              // and a growing answer stays in view by itself. Scrolled up, what
              // the reader looks at is held where it is while the answer grows.
              maintainVisibleContentPosition={showJump ? { minIndexForVisible: 0 } : undefined}
              onEndReached={() => {
                if (messages.hasNextPage && !messages.isFetchingNextPage) void messages.fetchNextPage()
              }}
              onEndReachedThreshold={0.5}
            onScrollToIndexFailed={(info) => list.current?.scrollToOffset({ offset: info.averageItemLength * info.index, animated: true })}
            />
            {showJump ? (
              <Pressable
                style={[styles.jump, { bottom: 12 + dockHeight, backgroundColor: colors.card, borderColor: colors.line }]}
                onPress={toEnd}
                accessibilityLabel={t('chat.scroll-bottom')}
              >
                <ArrowDown size={18} color={colors.text} />
              </Pressable>
            ) : null}
          </View>
        </ImageSource.Provider>
        </KeepInPlace.Provider>
      )}

      <Animated.View style={[styles.dock, dockSpace]} pointerEvents="box-none" onLayout={(event) => setDockHeight(event.nativeEvent.layout.height)}>
        {showQuestion ? (
          <AskUserDock
            key={asking.callId}
            questions={asking.questions}
            busy={answerQuestion.isPending}
            onAnswer={(answers) => handleAnswer(asking.callId, answers)}
            onDismiss={() => setDismissedCallId(asking.callId)}
          />
        ) : (
          <Composer
            value={input}
            onChange={setInput}
            onSend={() => void handleSend()}
            onStop={runId ? () => cancelRun.mutate(runId) : undefined}
            busy={working || createConversation.isPending}
            revising={Boolean(revising)}
            thinkingLevels={thinkingLevels}
            effort={effort}
            // The keyboard stays up: the slider stands just above it.
            onOpenEffort={() => setChoosingEffort(true)}
            onAttach={() => setAttaching(true)}
            hasTray={quotes.length > 0 || attachments.items.length > 0 || references.length > 0}
            tray={
              <>
              {quotes.map((quote, i) => (
                <QuoteCard key={i} title={quote.title} passage={quote.passage} onRemove={() => setQuote(null)} />
              ))}
              <AttachmentTray
                items={attachments.items}
                references={references}
                onRemove={attachments.remove}
                onUnrefer={(id) => setReferences((current) => current.filter((r) => r.conversationId !== id))}
              />
              </>
            }
          />
        )}
      </Animated.View>
      </View>
      </BlurTargetView>
      <DocumentScreen />
      <SubagentPanel />
      <Sheet open={listing} onClose={() => setListing(false)} title={t('document.menu.heading')}>
        {(documents.data ?? []).map((item) => (
          <SheetOption
            key={item.id}
            label={item.title}
            onPress={() => {
              setListing(false)
              openDocument(item.id)
            }}
          />
        ))}
      </Sheet>
      <AttachMenu
        open={attaching}
        onClose={() => setAttaching(false)}
        onFiles={(files) => void attachments.add(files)}
        canImages={accepts.image}
        // A message refers to at most five other conversations, as the server holds it to.
        onRefer={(reference) =>
          setReferences((current) => {
            if (current.some((r) => r.conversationId === reference.conversationId)) return current
            if (current.length >= MAX_REFERENCES) {
              toast.error(t('chat.attach.too-many'))
              return current
            }
            return [...current, reference]
          })
        }
        referred={references.map((r) => r.conversationId)}
        current={conversationId}
        web={canSearch ? { on: webSearch, onToggle: toggleWebSearch } : undefined}
      />
    </KeyboardAvoidingView>
      {/* Outside the KeyboardAvoidingView, over the whole screen; it keeps clear of the keyboard itself. */}
      <EffortSlider
        open={choosingEffort}
        levels={effortLevels}
        value={effort}
        onPick={pickEffort}
        onClose={closeEffort}
        blurTarget={page}
      />
    </View>
  )
}

/** How far below the header its veil runs on, thinning to nothing. */
const VEIL_FADE = 32

/** Conversations one message may refer to (the server's limit, `too_many_references`). */
const MAX_REFERENCES = 5

const Gap = () => <View style={styles.gap} />
/** A reply's Choices when the view offers them nothing. */
const NO_CHOICES: ChoicesActions = {}

const styles = StyleSheet.create({
  page: { flex: 1 },
  fill: { flex: 1 },
  veil: { position: 'absolute', top: 0, left: 0, right: 0 },
  header: { flexDirection: 'row', alignItems: 'center', paddingHorizontal: 14, paddingBottom: 6, gap: 8, zIndex: 1 },
  welcome: { flex: 1, justifyContent: 'center', paddingHorizontal: 28 },
  greeting: { fontSize: 26, lineHeight: 34, fontWeight: '500' },
  transcript: { paddingHorizontal: 18, paddingVertical: 16 },
  gap: { height: 20 },
  // A raised pill like the header's, so it stands off the text it floats over.
  jump: {
    position: 'absolute',
    alignSelf: 'center',
    width: 40,
    height: 40,
    borderRadius: 20,
    borderWidth: StyleSheet.hairlineWidth,
    alignItems: 'center',
    justifyContent: 'center',
    boxShadow: '0 4px 14px rgba(0, 0, 0, 0.1)',
  },
  dock: { paddingHorizontal: 12, paddingTop: 6 },
})
