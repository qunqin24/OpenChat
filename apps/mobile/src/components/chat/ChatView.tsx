import type { Message, ReasoningEffort } from '@openchat/api-client'
import { useQueryClient } from '@tanstack/react-query'
import { ArrowDown, Menu, SquarePen } from 'lucide-react-native'
import { useCallback, useEffect, useEffectEvent, useMemo, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { FlatList, Pressable, StyleSheet, Text, View, type NativeScrollEvent, type NativeSyntheticEvent } from 'react-native'
import { KeyboardAvoidingView } from 'react-native-keyboard-controller'
import { useSafeAreaInsets } from 'react-native-safe-area-context'

import { parseQuestions, type AskAnswer } from '@openchat/chat-core/ask-user'
import { greetingLines, pickGreeting } from '@openchat/chat-core/greeting'
import { defaultModelId } from '@openchat/chat-core/model-capacity'
import { composerMode, reviseDraft } from '@openchat/chat-core/revise'
import { isRunning } from '@openchat/chat-core/run-state'
import { startWroteNothing } from '@openchat/chat-core/start-failure'
import { displayTitle } from '@openchat/chat-core/titles'
import { buildTurns, pendingQuestion, regenerable, type AssistantTurn, type Turn } from '@openchat/chat-core/transcript'

import { useCompaction } from '@/hooks/compaction'
import { useConversation, useCreateConversation, useDeleteConversation } from '@/hooks/conversations'
import { useConversationEvents } from '@/hooks/events'
import { useMessages } from '@/hooks/messages'
import { freshModels, useChatFeatures, useChatModels, useUpdateConversationSettings } from '@/hooks/models'
import { useAnswerQuestion, useCancelRun, useEditMessage, useRegenerate, useSendMessage } from '@/hooks/send'
import { useComposerPrefs } from '@/lib/composer-prefs'
import { useUser } from '@/lib/session'
import { useChatStore } from '@/lib/store'
import { toast } from '@/lib/toast'
import { useColors } from '@/theme'
import { AskUserDock } from './AskUserDock'
import { Composer } from './Composer'
import { Divider } from './Divider'
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
  | { key: string; kind: 'divider'; label: string }
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
  const { t } = useTranslation()
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
  const [draftWebSearch, setDraftWebSearch] = useState<boolean>()
  const webSearch = conversation.data?.webSearch ?? draftWebSearch ?? saved.webSearch ?? true
  const canSearch = (features.data?.webSearch ?? false) && (modelById.get(modelId)?.toolCall ?? true)
  const toggleWebSearch = () => {
    const next = !webSearch
    setDraftWebSearch(next)
    savePrefs(userId, { webSearch: next })
    if (conversationId) updateSettings.mutate({ conversationId, webSearch: next })
  }
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
    const content = input.trim()
    if (!content) return
    const now = useChatStore.getState()
    if (working || createConversation.isPending || now.outbox[conversationId ?? ''] || (conversationId && isRunning(now.live[conversationId]))) return
    if (await capacityRefused()) return
    setInput('')
    const item = { content, attachments: [] }
    toEnd()

    if (conversationId && revising) {
      // The refused message is reworded, not followed: an edit of it.
      editMessage.mutate(
        { messageId: revising.id, content, attachments: [], ...choice() },
        { onError: () => setInput((current) => current || content) },
      )
      return
    }
    if (conversationId) {
      post(conversationId, item)
      sendMessage.mutate(
        { content, ...choice() },
        {
          onError: () => {
            unpost(conversationId)
            setInput((current) => current || content)
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
      setInput((current) => current || content)
      toast.error(t('chat.send-error.generic'))
      return
    }
    const madeId = created.id
    post(madeId, item)
    if (alive.current) onCreated(madeId)
    else unpost('', item)
    try {
      await sendMessage.mutateAsync({ conversationId: madeId, content, ...choice() })
    } catch (error) {
      // Refused before anything was written: the conversation was made for
      // this message alone, so it goes, and this is the new chat again.
      const wroteNothing = startWroteNothing(error)
      if (wroteNothing) deleteConversation.mutate(madeId)
      if (!alive.current) return
      setInput((current) => current || content)
      if (wroteNothing) onRemoved(madeId)
      else onSettled(madeId)
      return
    }
    if (alive.current) onSettled(madeId)
  }

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

  // The transcript, newest first for the inverted list.
  const rows = useMemo(() => {
    const out: Row[] = []
    for (const turn of turns) {
      out.push({ key: turn.key, kind: 'turn', turn })
      if (turn.after) out.push({ key: `${turn.key}:compaction`, kind: 'divider', label: t('chat.compaction.divider') })
    }
    if (replacing && !outgoing) out.push({ key: 'pending', kind: 'pending' })
    if (outgoing) out.push({ key: 'outbox', kind: 'outbox' }, { key: 'outbox-reply', kind: 'pending' })
    return out.reverse()
  }, [turns, replacing, outgoing, t])

  const isEmpty = !outgoing && (!conversationId || (!messages.isPending && turns.length === 0))
  const runId = live?.run?.run.status === 'running' ? live.run.run.id : undefined

  const renderRow = ({ item }: { item: Row }) => {
    if (item.kind === 'divider') return <Divider label={item.label} />
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
        />
      )
    }
    if (turn.kind === 'reports') return null
    if (turn.kind === 'refusal') return <RefusedReply modelId={modelId} model={modelById.get(modelId)} refusal={refusals[turn.message.id]} />
    const id = turnModel(turn) ?? modelId
    return (
      <Reply
        turn={turn}
        modelId={id}
        model={modelById.get(id)}
        askingCallId={askingCallId}
        onOpenQuestion={() => setDismissedCallId(null)}
        onRegenerate={turn === last?.answer && !working && !outgoing ? handleRegenerate : undefined}
      />
    )
  }

  const onScroll = (event: NativeSyntheticEvent<NativeScrollEvent>) => setShowJump(event.nativeEvent.contentOffset.y > BOTTOM_SLACK)

  const [greeting] = useState(() => pickGreeting(greetingLines(new Date(), Boolean(user?.name)), null))
  const title = conversationId ? displayTitle(conversation.data?.title, t) : ''
  const showQuestion = asking && dismissedCallId !== asking.callId

  return (
    <KeyboardAvoidingView behavior="padding" style={[styles.page, { backgroundColor: colors.bg }]}>
      <View style={[styles.header, { paddingTop: insets.top }]}>
        <Pressable hitSlop={8} style={styles.headerButton} onPress={onOpenDrawer} accessibilityLabel={t('chat.sidebar.open')}>
          <Menu size={22} color={colors.text} />
        </Pressable>
        <Text style={[styles.title, { color: colors.text }]} numberOfLines={1}>
          {title}
        </Text>
        <Pressable hitSlop={8} style={styles.headerButton} onPress={onNewChat} accessibilityLabel={t('chat.search.new-chat')}>
          <SquarePen size={21} color={colors.text} />
        </Pressable>
      </View>

      {isEmpty ? (
        <View style={styles.welcome}>
          <Text style={[styles.greeting, { color: colors.text }]}>{t(greeting.key, { name: user?.name ?? '' })}</Text>
        </View>
      ) : (
        <View style={styles.fill}>
          <FlatList
            ref={list}
            data={rows}
            inverted
            keyExtractor={(row) => row.key}
            renderItem={renderRow}
            contentContainerStyle={styles.transcript}
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
          />
          {showJump ? (
            <Pressable
              style={[styles.jump, { backgroundColor: colors.card, borderColor: colors.line }]}
              onPress={toEnd}
              accessibilityLabel={t('chat.scroll-bottom')}
            >
              <ArrowDown size={18} color={colors.text} />
            </Pressable>
          ) : null}
        </View>
      )}

      <View style={[styles.dock, { paddingBottom: Math.max(insets.bottom, 8) }]}>
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
            models={available}
            modelId={modelId}
            onPickModel={pickModel}
            thinkingLevels={thinkingLevels}
            effort={effort}
            onPickEffort={pickEffort}
            canSearch={canSearch}
            webSearch={webSearch}
            onToggleWebSearch={toggleWebSearch}
          />
        )}
      </View>
    </KeyboardAvoidingView>
  )
}

const Gap = () => <View style={styles.gap} />

const styles = StyleSheet.create({
  page: { flex: 1 },
  fill: { flex: 1 },
  header: { flexDirection: 'row', alignItems: 'center', paddingHorizontal: 8, gap: 4 },
  headerButton: { padding: 10 },
  title: { flex: 1, fontSize: 16, fontWeight: '500', textAlign: 'center' },
  welcome: { flex: 1, justifyContent: 'center', paddingHorizontal: 28 },
  greeting: { fontSize: 26, lineHeight: 34, fontWeight: '500' },
  transcript: { paddingHorizontal: 18, paddingVertical: 16 },
  gap: { height: 20 },
  jump: {
    position: 'absolute',
    alignSelf: 'center',
    bottom: 12,
    width: 38,
    height: 38,
    borderRadius: 19,
    borderWidth: StyleSheet.hairlineWidth,
    alignItems: 'center',
    justifyContent: 'center',
  },
  dock: { paddingHorizontal: 12, paddingTop: 6 },
})
