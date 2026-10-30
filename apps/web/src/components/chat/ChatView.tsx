import { useQueryClient } from '@tanstack/react-query'
import { useNavigate } from '@tanstack/react-router'
import { ArrowDown } from 'lucide-react'
import { Fragment, useCallback, useEffect, useEffectEvent, useLayoutEffect, useMemo, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { type ConversationReference, type Message, type ReasoningEffort } from '@openchat/api-client'
import {
  chatModelsQueryOptions,
  useChatFeatures,
  useChatModels,
  useUpdateConversationSettings,
} from '@/hooks/use-chat-models'
import { useCompaction, useUndoCompaction } from '@/hooks/use-compaction'
import { useConversationEvents } from '@/hooks/use-conversation-events'
import { useConversation, useCreateConversation, useDeleteConversation } from '@/hooks/use-conversations'
import { useAttachments } from '@/hooks/use-attachments'
import { useFileDrop } from '@/hooks/use-file-drop'
import { useMessages } from '@/hooks/use-messages'
import {
  useAnswerQuestion,
  useCancelRun,
  useEditMessage,
  useRegenerate,
  useSendMessage,
} from '@/hooks/use-send-message'
import { useSession } from '@/features/auth/use-auth'
import { cn } from '@/lib/utils'
import { loadComposerPrefs, saveComposerPrefs } from '@/lib/composer-prefs'
import { defaultModelId } from '@openchat/chat-core/model-capacity'
import { prefersReducedMotion, smoothScrollBehavior } from '@/lib/motion'
import { displayTitle } from '@openchat/chat-core/titles'
import { acceptAttribute, acceptsOf, TEXT_TYPE } from '@openchat/chat-core/attachments'
import { isRunning } from '@openchat/chat-core/run-state'
import { composerMode, reviseDraft } from '@openchat/chat-core/revise'
import { returnableFiles, startWroteNothing } from '@openchat/chat-core/start-failure'
import { buildTurns, pendingQuestion, regenerable } from '@openchat/chat-core/transcript'
import { turnOutline } from '@openchat/chat-core/outline'
import { parseQuestions, type AskAnswer } from '@openchat/chat-core/ask-user'
import { useChatStore } from '@/stores/chat-store'
import AskUserDock from './AskUserDock'
import CompactionDivider from './CompactionDivider'
import Composer from './Composer'
import ContextMeter from './ContextMeter'
import { DropOverlay } from './DropOverlay'
import { ChoicesContext, type ChoicesActions } from './genui/choices-context'
import { AssistantTurn, PendingReply, RefusedReply, UserMessage } from './MessageItem'
import { TurnNavigator } from './TurnNavigator'
import ModelPicker from './ModelPicker'
import ThinkingPicker from './ThinkingPicker'
import WebSearchToggle from './WebSearchToggle'
import { SidebarToggle } from './ChatShell'
import TraceView from './TraceView'
import DocumentPanel from './DocumentPanel'
import { useDocumentPanel } from '@/stores/document-panel-store'
import { API_BASE_URL } from '@/lib/api'
import { ImageSource, proxiedImages } from './genui/image-source'
import SubagentPanel from './SubagentPanel'
import SubagentSummary from './SubagentSummary'
import PanelResizer from './PanelResizer'
import DocumentsMenu from './DocumentsMenu'
import SubagentsButton from './SubagentsButton'
import { loadPanelWidth, savePanelWidth } from '@/lib/panel-width'
import { SubagentReportLines } from './SubagentLines'
import { useSubagentPanel } from '@/stores/subagent-panel-store'
import { useFilePanel } from '@/stores/file-panel-store'
import FilePanel from './FilePanel'
import { useDocumentQuote } from '@/stores/document-quote-store'
import { answerQuote, quoteMessage } from '@openchat/chat-core/document-selection'
import { gapLabel, timeGaps } from '@openchat/chat-core/time-gap'
import AskSelection from './AskSelection'
import { TemporaryBar, TemporaryToggle } from './TemporaryChat'
import Welcome, { WelcomeStarters } from './Welcome'

/** Distance from the bottom, in px, still counted as "parked at the tail". */
const BOTTOM_SLACK = 24
/** Turns drawn when a conversation opens, and drawn more of each time the reader nears the top. */
const TURN_STEP = 20
/** How close to the top, in px, the reader must be before older turns are drawn (or loaded). */
const TOP_REACH = 800
/** A reply's Choices when the page offers them nothing (genui/choices.tsx). */
const NO_CHOICES: ChoicesActions = {}

interface ChatViewProps {
  /** The conversation shown; none on the new-chat screen. */
  conversationId?: string
  /**
   * The first message of a new chat made a conversation, which this page shows
   * from now on (ChatPage). The address follows when the server has answered.
   */
  onCreated: (conversationId: string) => void
  /**
   * The conversation made for the first message was removed, the server having
   * refused the message before writing anything: this is the new-chat screen again.
   */
  onRemoved: (conversationId: string) => void
}

export function ChatView({ conversationId, onCreated, onRemoved }: ChatViewProps) {
  const { t, i18n } = useTranslation()
  const navigate = useNavigate()
  const queryClient = useQueryClient()

  const [input, setInput] = useState('')
  const scrollRef = useRef<HTMLDivElement>(null)
  const columnRef = useRef<HTMLDivElement>(null)
  const parked = useRef(true)
  const [showJump, setShowJump] = useState(false)
  // The 轨迹 tab an administrator opened instead of the transcript.
  const [traceOpen, setTraceOpen] = useState(false)

  const conversation = useConversation(conversationId ?? '')
  const messages = useMessages(conversationId ?? '')
  const models = useChatModels()
  const createConversation = useCreateConversation()
  const sendMessage = useSendMessage(conversationId ?? '')
  const cancelRun = useCancelRun(conversationId ?? '')
  const regenerate = useRegenerate(conversationId ?? '')
  const editMessage = useEditMessage(conversationId ?? '')
  const answerQuestion = useAnswerQuestion(conversationId ?? '')
  const summary = useCompaction(conversationId)
  const undoCompaction = useUndoCompaction(conversationId ?? '')
  const updateSettings = useUpdateConversationSettings()

  // Attaches to whatever this conversation is running — including a run that
  // kept going while the page was closed — and follows it.
  useConversationEvents(conversationId)

  const live = useChatStore((s) => (conversationId ? s.live[conversationId] : undefined))
  const outgoing = useChatStore((s) => s.outbox[conversationId ?? ''])
  const post = useChatStore((s) => s.post)
  const unpost = useChatStore((s) => s.unpost)
  // What the responses that refused sends in this page said (the strike warning),
  // by the refused message.
  const refusals = useChatStore((s) => s.refusals)
  const deleteConversation = useDeleteConversation()
  const temporaryChat = useChatStore((s) => s.temporaryChat)
  const setTemporaryChat = useChatStore((s) => s.setTemporaryChat)
  const replacing = useChatStore((s) => s.replacing[conversationId ?? ''])

  // Anything that makes the model's open question moot, as writing something
  // else does, or is about to: a run going, a message or a redo on its way.
  const superseded =
    isRunning(live) || sendMessage.isPending || regenerate.isPending || editMessage.isPending || Boolean(outgoing) || Boolean(replacing)
  const working = superseded || answerQuestion.isPending

  // What the composer was last set to, anywhere, for a new chat to open with.
  const session = useSession().data
  const userId = session?.id
  const saved = useMemo(() => loadComposerPrefs(userId), [userId])

  // The model: what this conversation remembers, else what was picked on the
  // empty screen, else the last one picked, else the first one offered — each
  // only while it is still offered, since a model an administrator has taken
  // away (or kept for administrators) would only be refused. Until the list
  // arrives the conversation's own is trusted, so the picker does not flicker.
  //
  // A model at its spending cap is still offered (the picker shows it
  // disabled), so a conversation's own or a deliberate pick keeps it: swapping
  // the model under someone would answer a question with something they did not
  // choose. Only the last two, which nobody chose for this chat, steer around a
  // cap (defaultModelId), and sending on a capped model says why it cannot.
  const available = useMemo(() => models.data ?? [], [models.data])
  const [draftModel, setDraftModel] = useState<string>()
  const modelById = useMemo(() => new Map(available.map((m) => [m.id, m])), [available])
  const offered = (id: string | null | undefined) =>
    id && (models.data === undefined || modelById.has(id)) ? id : undefined
  const modelId =
    offered(conversation.data?.model) ?? offered(draftModel) ?? defaultModelId(available, saved.model)

  // Files: only what the selected model takes. With none that does, the button
  // and the drop zone say so rather than going quiet.
  // Text files go to any model as words.
  const accepts = useMemo(() => acceptsOf(modelById.get(modelId)), [modelById, modelId])
  const anyModelTakesFiles = available.some((m) => acceptsOf(m).image || acceptsOf(m).pdf)
  const attachments = useAttachments(accepts)
  // Other conversations the message is to refer to ("+" → 引用其他对话): not
  // files, so nothing goes up; the server reads them when the model is asked.
  const [references, setReferences] = useState<ConversationReference[]>([])
  const refer = useCallback((reference: ConversationReference) => {
    setReferences((current) =>
      current.some((r) => r.conversationId === reference.conversationId) ? current : [...current, reference],
    )
  }, [])
  const unrefer = useCallback((id: string) => {
    setReferences((current) => current.filter((r) => r.conversationId !== id))
  }, [])
  const uploadHint = accepts.text
    ? accepts.image
      ? accepts.pdf
        ? t('chat.file.upload-all')
        : t('chat.file.upload-image-text')
      : accepts.pdf
        ? t('chat.file.upload-pdf-text')
        : t('chat.file.upload-text')
    : accepts.image
      ? accepts.pdf
        ? t('chat.file.upload-both')
        : t('chat.file.upload-image')
      : accepts.pdf
        ? t('chat.file.upload-pdf')
        : anyModelTakesFiles
          ? t('chat.file.unsupported')
          : t('chat.file.none')
  const canAttach = accepts.image || accepts.pdf || accepts.text
  const fileDrop = useFileDrop((files) => {
    if (canAttach) attachments.add(files)
    else toast.error(uploadHint, { id: 'file-upload-unavailable' })
  })

  const pickModel = useCallback(
    (id: string) => {
      setDraftModel(id)
      saveComposerPrefs(userId, { model: id })
      if (conversationId) updateSettings.mutate({ conversationId, model: id })
    },
    [conversationId, updateSettings, userId],
  )

  // Reasoning effort, remembered the same way. Offered only for a model the
  // catalogue says reasons; the server drops it for one it knows does not.
  const [draftEffort, setDraftEffort] = useState<ReasoningEffort>()
  const effort = conversation.data?.reasoningEffort ?? draftEffort ?? saved.effort ?? 'auto'
  // The model's own thinking levels, from models.dev via the catalogue.
  const thinkingLevels = useMemo(() => modelById.get(modelId)?.reasoningEfforts ?? [], [modelById, modelId])
  const canThink = thinkingLevels.length > 0
  const pickEffort = useCallback(
    (next: ReasoningEffort) => {
      setDraftEffort(next)
      saveComposerPrefs(userId, { effort: next })
      if (conversationId) updateSettings.mutate({ conversationId, reasoningEffort: next })
    },
    [conversationId, updateSettings, userId],
  )

  // Web access, remembered the same way. Offered when the server has web
  // search on and the model may call tools; off, the run gets no web tools.
  const features = useChatFeatures()
  const [draftWebSearch, setDraftWebSearch] = useState<boolean>()
  const webSearch = conversation.data?.webSearch ?? draftWebSearch ?? saved.webSearch ?? true
  const canSearch = (features.data?.webSearch ?? false) && (modelById.get(modelId)?.toolCall ?? true)
  const pickWebSearch = useCallback(
    (next: boolean) => {
      setDraftWebSearch(next)
      saveComposerPrefs(userId, { webSearch: next })
      if (conversationId) updateSettings.mutate({ conversationId, webSearch: next })
    },
    [conversationId, updateSettings, userId],
  )

  // Pictures an answer gives by address come through the server, for this
  // conversation only; described ones are found by it (found-image.ts).
  const images = useMemo(
    () => (conversationId ? proxiedImages(API_BASE_URL, conversationId) : () => null),
    [conversationId],
  )

  const turns = useMemo(
    () => buildTurns(messages.data?.items ?? [], live?.run ?? null, replacing, summary),
    [messages.data, live, replacing, summary],
  )
  // Where the person picked the conversation up again after a pause: a time
  // above their message, as the model is told it (lib/time-gap.ts).
  const gaps = useMemo(() => timeGaps(turns), [turns])
  // "Today" and "yesterday" are from when the page opened.
  const [openedAt] = useState(() => Date.now())

  // Only the end of a long transcript is drawn: the last turns, then 20 more
  // each time the reader scrolls near the top, then — once the drawn turns
  // reach the oldest loaded one — the next older page from the server. The
  // window's top is a turn, not a count, so turns arriving at the end do not
  // push the one at the top away from a reader who has scrolled up.
  const hasOlder = messages.hasNextPage
  // A page can begin in the middle of a reply (its question is in the page
  // before): that half-turn waits for its other half rather than being drawn.
  const floor = hasOlder && turns[0]?.kind === 'assistant' ? 1 : 0
  const [topKey, setTopKey] = useState<string>()
  const topIndex = topKey ? turns.findIndex((turn) => turn.key === topKey) : -1
  const start = Math.max(floor, topIndex >= 0 ? topIndex : turns.length - TURN_STEP)
  const outline = useMemo(() => turnOutline(turns.slice(start)), [turns, start])
  const anchor = useRef<{ el: HTMLElement; top: number } | null>(null)

  // While the conversation ends on a message moderation refused, it does not go
  // on until that message is reworded: the composer is its editor (sending from it
  // is an edit of the refused message), and nothing else is offered below.
  const mode = useMemo(() => composerMode(turns), [turns])
  const revising = mode.kind === 'revise' ? mode.message : null
  // A writing block's passage waiting in this conversation's composer.
  const quoteState = useDocumentQuote()
  const quotes = useMemo(
    () => quoteState.quotes.filter((q) => q.conversationId === conversationId),
    [quoteState.quotes, conversationId],
  )
  const setQuote = quoteState.set

  // The last answer can be asked for again (not while a refused message ends the
  // conversation: it is reworded first).
  const last = useMemo(() => regenerable(turns), [turns])

  // The question the last reply ended on, if nobody has answered or written past it.
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
  // The panel put away for this question (Skip, ✕ or Escape); the line in the
  // reply brings it back.
  const [dismissedCallId, setDismissedCallId] = useState<string | null>(null)
  const dock = useRef<HTMLDivElement>(null)
  const dismissQuestion = () => {
    if (asking) setDismissedCallId(asking.callId)
    dock.current?.querySelector<HTMLTextAreaElement>('textarea')?.focus()
  }
  const handleAnswer = (callId: string, answers: AskAnswer[]) => {
    parked.current = true
    answerQuestion.mutate({
      callId,
      answers,
      model: modelId || undefined,
      reasoningEffort: canThink ? effort : undefined,
      webSearch: canSearch ? webSearch : undefined,
    })
  }

  const handleRegenerate = () => {
    if (!last) return
    parked.current = true
    regenerate.mutate({
      inputId: last.inputId,
      model: modelId || undefined,
      reasoningEffort: canThink ? effort : undefined,
      webSearch: canSearch ? webSearch : undefined,
    })
  }

  // Taking the compression back is not offered under a run: it would change
  // the context in the middle of the run's tool loop.
  const divider = {
    onUndo: !working && !outgoing ? () => undoCompaction.mutate() : undefined,
    undoing: undoCompaction.isPending,
  }

  // Any question can be reworded and asked again: the old wording and the turns
  // after it are deleted, and the new wording goes on the end.
  const editQuestion = (message: Message, content: string) => {
    parked.current = true
    setRefusedEdit(null)
    editMessage.mutate(
      {
        messageId: message.id,
        content,
        attachments: message.attachments ?? [],
        references: message.references,
        model: modelId || undefined,
        reasoningEffort: canThink ? effort : undefined,
        webSearch: canSearch ? webSearch : undefined,
      },
      // Refused (content moderation, a limit): the question is back as it was,
      // and the field reopens with the words that were typed.
      { onError: () => setRefusedEdit({ id: message.id, content }) },
    )
  }
  const [refusedEdit, setRefusedEdit] = useState<{ id: string; content: string } | null>(null)

  const isEmpty = !outgoing && (!conversationId || (!messages.isPending && turns.length === 0))

  // Leaving the new-chat page (`.n-main.is-welcome`), the composer glides from
  // the middle of the page to its foot rather than jumping there: where it
  // stood is noted after every layout, and the frame the page stops being a
  // welcome it is moved back there and let go (its width follows in CSS).
  const dockTop = useRef<number | null>(null)
  const wasWelcome = useRef(isEmpty)
  useLayoutEffect(() => {
    const element = dock.current
    if (!element) return
    const top = element.getBoundingClientRect().top
    const from = dockTop.current
    if (wasWelcome.current && !isEmpty && from !== null && Math.abs(from - top) > 4 && !prefersReducedMotion()) {
      element.animate([{ transform: `translateY(${from - top}px)` }, { transform: 'none' }], {
        duration: DOCK_GLIDE_MS,
        easing: 'cubic-bezier(0.22, 1, 0.36, 1)',
      })
    }
    wasWelcome.current = isEmpty
    dockTop.current = top
  })

  // Grows as the reply streams, so the tail-follow below reacts to tokens and
  // not only to new rows.
  const streamed = (live?.run?.partial ?? []).reduce(
    (n, part) => n + (part.type === 'tool_call' ? part.rawArguments.length : 'text' in part ? part.text.length : 0),
    0,
  )

  const scrollToBottom = useCallback((behavior: ScrollBehavior = smoothScrollBehavior()) => {
    const el = scrollRef.current
    if (!el) return
    el.scrollTo({ top: el.scrollHeight, behavior })
    parked.current = true
    setShowJump(false)
  }, [])

  // The furthest down the view has been since it was last parked at the end.
  const peak = useRef(0)
  const handleScroll = () => {
    const el = scrollRef.current
    if (!el) return
    const distance = el.scrollHeight - el.scrollTop - el.clientHeight
    // Only the reader going up lets go of the end. A scroll that is still on
    // its way down (the jump button's smooth scroll) passes through positions
    // short of the end, and the reply keeps growing under it; taking those for
    // "the reader left" stopped following the moment the button was pressed.
    // Up is measured from the furthest point down, not from the last event: a
    // trackpad read slowly moves a pixel or less per event, and each step
    // alone never looked like going up.
    if (distance <= BOTTOM_SLACK) {
      parked.current = true
      peak.current = el.scrollTop
    } else if (parked.current) {
      if (el.scrollTop < peak.current - 1) parked.current = false
      else peak.current = Math.max(peak.current, el.scrollTop)
    }
    // Not following: the peak is wherever the view is, so that parking it again
    // (the jump button, a send) measures from there and not from an old peak.
    if (!parked.current) peak.current = el.scrollTop
    setShowJump(!parked.current)
    loadOlder()
  }

  // Whenever the column or the screen changes size: a reader parked at the end
  // is kept there, and one who is now at the end is parked again.
  //
  // Kept there: a reply grows not only when a batch arrives - a formula, a
  // diagram or highlighted code is drawn after its text, a section opens -
  // and following only on new tokens left the view trailing by a few lines
  // and the last lines of an answer below the screen.
  //
  // Parked again: the transcript can stop overflowing without a scroll (a
  // section the reader opened, scrolled through and folded away again leaves it
  // shorter than the screen), and the browser moves nothing - so no scroll event
  // ever took down the jump button it had put up.
  const transcriptShown = !isEmpty && !(conversationId && messages.isPending && !outgoing)

  // The reader going up, told by what they do rather than by where the view
  // lands: while a reply grows, following moves the view down every frame, so
  // a slow read upwards (a trackpad, a pixel at a time) was overtaken before
  // it ever showed as a position above the end. Any of these lets go at once;
  // reaching the end again (handleScroll) takes it back.
  useEffect(() => {
    const scroller = scrollRef.current
    if (!transcriptShown || !scroller) return
    // Only a view that can go up lets go, and the button follows where the view
    // then is, not the gesture: a wheel or key up with nothing above (a short
    // transcript, already at the top), or a press on the scrollbar that moves
    // nothing, put the button up over a view still at the end - and stopped
    // following, so a reply growing past the screen went on below it unseen.
    let check = 0
    const leave = () => {
      if (!parked.current || scroller.scrollTop <= 0) return
      parked.current = false
      cancelAnimationFrame(check)
      check = requestAnimationFrame(() => {
        if (parked.current) return
        if (scroller.scrollHeight - scroller.scrollTop - scroller.clientHeight <= BOTTOM_SLACK) parked.current = true
        setShowJump(!parked.current)
      })
    }
    const onWheel = (event: WheelEvent) => {
      if (event.deltaY < 0) leave()
    }
    let touchY: number | null = null
    const onTouchStart = (event: TouchEvent) => {
      touchY = event.touches[0]?.clientY ?? null
    }
    const onTouchMove = (event: TouchEvent) => {
      const y = event.touches[0]?.clientY
      // The finger moving down scrolls the transcript up.
      if (touchY !== null && y !== undefined && y > touchY + 2) leave()
    }
    // Keys scroll the transcript only when nothing else has the focus: arrows
    // in a menu, a picker or a field move through that, and showed the button.
    const onKeyDown = (event: KeyboardEvent) => {
      const target = event.target
      if (target instanceof Node && target !== document.body && !scroller.contains(target)) return
      if (target instanceof HTMLElement && target.closest('textarea, input, select, button, [contenteditable], [role]')) return
      if (event.key === 'ArrowUp' || event.key === 'PageUp' || event.key === 'Home') leave()
    }
    // A press on the scroller itself, not on a message: its scrollbar.
    const onPointerDown = (event: PointerEvent) => {
      if (event.target === scroller) leave()
    }
    scroller.addEventListener('wheel', onWheel, { passive: true })
    scroller.addEventListener('touchstart', onTouchStart, { passive: true })
    scroller.addEventListener('touchmove', onTouchMove, { passive: true })
    scroller.addEventListener('pointerdown', onPointerDown)
    window.addEventListener('keydown', onKeyDown)
    return () => {
      cancelAnimationFrame(check)
      scroller.removeEventListener('wheel', onWheel)
      scroller.removeEventListener('touchstart', onTouchStart)
      scroller.removeEventListener('touchmove', onTouchMove)
      scroller.removeEventListener('pointerdown', onPointerDown)
      window.removeEventListener('keydown', onKeyDown)
    }
  }, [transcriptShown])
  useEffect(() => {
    const scroller = scrollRef.current
    const column = columnRef.current
    if (!transcriptShown || !scroller || !column) return
    const observer = new ResizeObserver(() => {
      if (parked.current) {
        scroller.scrollTop = scroller.scrollHeight
        return
      }
      if (scroller.scrollHeight - scroller.scrollTop - scroller.clientHeight > BOTTOM_SLACK) return
      parked.current = true
      setShowJump(false)
    })
    observer.observe(column)
    observer.observe(scroller)
    return () => observer.disconnect()
  }, [transcriptShown])

  // Pins the window's top once there is a transcript to window.
  const pinned = turns[start]
  if (topKey === undefined && !messages.isPending && pinned) setTopKey(pinned.key)

  // Notes which message sits at the top of the screen and where, to put it
  // back once turns have been added above it. The first one that starts below
  // the top bar, not the one cut off by it: a message partly out of sight can
  // itself change height as the browser draws it, and the place held would
  // move with it.
  const holdPlace = () => {
    const scroller = scrollRef.current
    if (!scroller) return
    const top = scroller.getBoundingClientRect().top
    const shown = top + parseFloat(getComputedStyle(scroller).paddingTop)
    let held: HTMLElement | undefined
    for (const child of columnRef.current?.children ?? []) {
      if (!(child instanceof HTMLElement) || child.dataset.edge) continue
      const rect = child.getBoundingClientRect()
      if (rect.bottom <= top) continue
      held = child
      if (rect.top >= shown) break
    }
    if (held) anchor.current = { el: held, top: held.getBoundingClientRect().top - top }
  }

  // Draws older turns when the reader is near the top: more of what is loaded,
  // else the next page of it. Run on scrolling, and again whenever what it
  // reads changes, so it goes on until the reader is no longer near the top.
  const loadOlder = () => {
    const scroller = scrollRef.current
    if (!scroller || messages.isPending || scroller.scrollTop >= TOP_REACH) return
    const above = turns[Math.max(floor, start - TURN_STEP)]
    if (start > floor && above) {
      holdPlace()
      setTopKey(above.key)
    } else if (hasOlder && !messages.isFetchingNextPage && !messages.isFetchNextPageError) {
      void messages.fetchNextPage()
    }
  }
  const loadOlderAfterChange = useEffectEvent(loadOlder)
  useEffect(() => {
    loadOlderAfterChange()
  }, [start, floor, turns.length, hasOlder, messages.isFetchingNextPage, messages.isPending])

  // Turns added above keep the message the reader is looking at where it was.
  // A browser's own scroll anchoring may have done it already (the shift
  // measured here is then zero); Safari has none. And turns drawn above
  // are `content-visibility: auto`, laid out at an estimated height until the
  // browser decides they are near enough to draw — which it does after this
  // runs. So for a moment the column's size is watched and the place put back
  // whenever it changes, ending at the reader's first touch.
  useLayoutEffect(() => {
    const held = anchor.current
    anchor.current = null
    const scroller = scrollRef.current
    const column = columnRef.current
    if (!held || !scroller || !column) return
    // Where the scroll position was left. Found elsewhere, someone else moved
    // it — the reader, a jump to the end, the browser's own anchoring — and
    // this lets go.
    let left = scroller.scrollTop
    const restore = () => {
      if (!held.el.isConnected || Math.abs(scroller.scrollTop - left) > 1) return stop()
      const shift = held.el.getBoundingClientRect().top - scroller.getBoundingClientRect().top - held.top
      if (shift) scroller.scrollTop += shift
      left = scroller.scrollTop
    }
    restore()
    const observer = new ResizeObserver(restore)
    observer.observe(column)
    const touches = ['wheel', 'touchstart', 'pointerdown', 'keydown'] as const
    const timer = window.setTimeout(stop, 1000)
    function stop() {
      observer.disconnect()
      window.clearTimeout(timer)
      for (const type of touches) scroller?.removeEventListener(type, stop)
    }
    for (const type of touches) scroller.addEventListener(type, stop, { passive: true })
    return stop
  }, [start])

  // Follow the tail while the reader has not scrolled away from it.
  useLayoutEffect(() => {
    // The landing page is its own scrollable document on short screens; opening
    // it at the bottom would hide the model heading and the input above it.
    if (!isEmpty && parked.current) scrollToBottom('auto')
  }, [isEmpty, turns.length, streamed, outgoing, replacing, scrollToBottom])

  // The refused message the composer was filled with, so that a person who
  // empties the box is not given its words back. Empty on arrival (a refresh,
  // another device), the box takes them; refused from this box, they are there.
  const filledFor = useRef<string | null>(null)
  const reviseId = revising?.id ?? null
  const fillRevision = useEffectEvent(() => {
    const next = reviseDraft(mode, input, filledFor.current)
    filledFor.current = next.handled
    if (next.fill !== null) setInput(next.fill)
  })
  useEffect(() => {
    fillRevision()
  }, [reviseId])

  // Whether this page is still on screen. A send that was under way when the
  // reader opened another conversation finishes in the background, and must not
  // touch the page that has replaced this one.
  const alive = useRef(true)
  useEffect(() => {
    alive.current = true
    return () => {
      alive.current = false
    }
  }, [])

  // Whether the model is at its spending cap, said to the reader if so. The
  // list may be a minute old, or kept from an earlier visit, so it is asked
  // again before refusing - the cap may have been raised since.
  const capacityRefused = useCallback(async () => {
    if (!modelById.get(modelId)?.atCapacity) return false
    const fresh = await queryClient.fetchQuery({ ...chatModelsQueryOptions, staleTime: 0 }).catch(() => undefined)
    // Could not be asked: let the server decide, as it always does.
    if (!fresh?.find((m) => m.id === modelId)?.atCapacity) return false
    toast.error(t('chat.send-error.model_capacity_reached'), { id: 'model-capacity' })
    return true
  }, [modelById, modelId, queryClient, t])

  const handleSend = useCallback(async () => {
    const typed = input.trim()
    if (!typed || attachments.uploading || attachments.failed) return
    // A passage of a writing block quoted for this request goes with it, in
    // front (lib/document-selection), unless this is the rewording of a
    // refused message, which keeps its own words.
    const quoted = quotes.length > 0 && !revising ? quotes : null
    const content = quoted ? quoteMessage(quoted, typed) : typed
    // An answer is being made, or a message is on its way: one at a time, as the
    // server says (`conversation_busy`). Read from the store, not from this
    // render, so a second Enter in the same moment finds the first.
    const now = useChatStore.getState()
    if (
      working ||
      createConversation.isPending ||
      now.outbox[conversationId ?? ''] ||
      (conversationId && isRunning(now.live[conversationId]))
    ) {
      return
    }
    // The model is at its cap: nothing is sent, and nothing is made for it (a
    // new chat would otherwise be created only to be refused).
    if (await capacityRefused()) return
    const attachmentIds = attachments.ids
    const referred = references
    const conversationIds = referred.map((r) => r.conversationId)
    // Back in the box with the words when the send fails, unless others were
    // chosen meanwhile.
    const referBack = () => setReferences((current) => (current.length > 0 ? current : referred))
    setInput('')
    setReferences([])
    // Out of the box at once, as the text is: the message shows them now. Back
    // in the box if the send fails.
    const taken = attachments.take()
    if (quoted) setQuote(null)
    // Shown at once, at the bottom, while the request is on its way.
    const item = {
      content,
      attachments: attachments.items
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
    parked.current = true

    if (conversationId && revising) {
      // The refused message is reworded, not followed: an edit of it, which
      // takes the files in the box with it and the new words checked again.
      parked.current = true
      editMessage.mutate(
        {
          messageId: revising.id,
          content,
          attachments: item.attachments,
          references: referred,
          attachmentIds,
          conversationIds,
          model: modelId || undefined,
          reasoningEffort: canThink ? effort : undefined,
          webSearch: canSearch ? webSearch : undefined,
        },
        {
          onSuccess: () => attachments.release(taken),
          // Refused again or not taken: the words and the files go back in the box.
          onError: () => {
            setInput((current) => current || content)
            attachments.restore(taken)
            referBack()
          },
        },
      )
      return
    }

    if (conversationId) {
      post(conversationId, item)
      sendMessage.mutate(
        {
          content,
          model: modelId || undefined,
          reasoningEffort: canThink ? effort : undefined,
          webSearch: canSearch ? webSearch : undefined,
          attachmentIds,
          conversationIds,
        },
        {
          // A refused message comes back as it was written, files and all.
          onSuccess: () => attachments.release(taken),
          onError: () => {
            unpost(conversationId)
            setInput((current) => current || content)
            attachments.restore(taken)
            referBack()
          },
        },
      )
      return
    }

    // No conversation yet: make one with the chosen model. This page takes it as
    // its own at once - the message stays where it is, and the answer grows under
    // it - and sends the message to it; the address follows when the server has
    // answered (below). Nothing is handed to another page, because there is none.
    post('', item)
    let created
    try {
      created = await createConversation.mutateAsync({
        ...(modelId ? { model: modelId } : {}),
        ...(canThink && effort !== 'auto' ? { reasoningEffort: effort } : {}),
        ...(canSearch && !webSearch ? { webSearch: false } : {}),
        ...(temporaryChat ? { temporary: true } : {}),
      })
    } catch {
      // Nothing was made or sent: the text comes back, and the files never left.
      // Only this send's message: the page may have started over meanwhile, and
      // a new first message may be on the empty key now.
      unpost('', item)
      if (!alive.current) return
      setInput((current) => current || content)
      attachments.restore(taken)
      referBack()
      toast.error(t('chat.send-error.generic'))
      return
    }
    const madeId = created.id
    // Once made, the conversation itself says it is temporary; the switch
    // starts the next new chat off again.
    setTemporaryChat(false)
    // The message under the conversation's own key, which the page reads from
    // the moment it shows the conversation. (The empty key is let go of then,
    // not before: a frame without a message would draw the welcome again.)
    post(madeId, item)
    if (alive.current) onCreated(madeId)
    else unpost('', item)

    try {
      await sendMessage.mutateAsync({
        conversationId: madeId,
        content,
        model: modelId || undefined,
        reasoningEffort: canThink ? effort : undefined,
        webSearch: canSearch ? webSearch : undefined,
        attachmentIds,
        conversationIds,
      })
    } catch (error) {
      // What went wrong has been said (the mutation's own error handler).
      // Refused before anything was written (a limit, no model): the conversation
      // was made for this message alone and is empty, so it goes rather than
      // leave a blank chat in the list, and this is the new-chat screen again,
      // with the text and the files as they were. Any other failure keeps the
      // conversation, which may hold the message after all - as it does for a
      // message content moderation refused, which is kept in the conversation
      // (shown there by `useSendMessage`): the address follows, and the text and
      // the files are in its composer, which is that message's editor.
      const wroteNothing = startWroteNothing(error)
      if (wroteNothing) deleteConversation.mutate(madeId)
      if (!alive.current) return
      // Uploads the server says it cannot use would only be refused again.
      const usable = returnableFiles(error, taken)
      attachments.release(taken.filter((file) => !usable.includes(file)))
      attachments.restore(usable)
      referBack()
      setInput((current) => current || content)
      if (wroteNothing) onRemoved(madeId)
      else await navigate({ to: '/chat/$conversationId', params: { conversationId: madeId }, replace: true })
      return
    }
    attachments.release(taken)
    if (!alive.current) return
    // `replace`: the new-chat screen was a draft, not a place to come back to.
    await navigate({ to: '/chat/$conversationId', params: { conversationId: madeId }, replace: true })
  }, [
    attachments,
    canSearch,
    canThink,
    capacityRefused,
    conversationId,
    createConversation,
    deleteConversation,
    editMessage,
    effort,
    input,
    modelId,
    navigate,
    onCreated,
    onRemoved,
    post,
    quotes,
    references,
    revising,
    sendMessage,
    setQuote,
    working,
    setTemporaryChat,
    t,
    temporaryChat,
    unpost,
    webSearch,
  ])

  // A choice picked in the last answer (genui/choices.tsx): sent as the
  // reader's next message, the way the composer sends one, leaving whatever is
  // in the composer where it is. Not while a refused message is being reworded.
  const handlePick = useCallback(
    async (content: string) => {
      if (!conversationId || revising) return
      const now = useChatStore.getState()
      if (working || now.outbox[conversationId] || isRunning(now.live[conversationId])) return
      if (await capacityRefused()) return
      const item = { content, attachments: [] }
      parked.current = true
      post(conversationId, item)
      sendMessage.mutate(
        {
          content,
          model: modelId || undefined,
          reasoningEffort: canThink ? effort : undefined,
          webSearch: canSearch ? webSearch : undefined,
        },
        {
          // Not taken: the message goes to the composer, to send again or change.
          // (A message moderation refused stays as the one to reword instead.)
          onError: () => {
            unpost(conversationId, item)
            setInput((current) => current || content)
          },
        },
      )
    },
    [canSearch, canThink, capacityRefused, conversationId, effort, modelId, post, revising, sendMessage, unpost, webSearch, working],
  )

  // What was sent after each reply, for its Choices blocks to mark the choice
  // it was (genui/choices.tsx). Only the last answer's can send (below).
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

  const handleStop = useCallback(() => {
    const runId = live?.run?.run.id
    if (runId) cancelRun.mutate(runId)
  }, [cancelRun, live])

  // The message of a first send was shown under the empty key until the page
  // took the conversation as its own; now it is under the conversation's.
  useEffect(() => {
    if (conversationId) unpost('')
  }, [conversationId, unpost])

  // A temporary chat, made or about to be: the chat is drawn in its dark frame.
  const temporaryMode = conversationId ? Boolean(conversation.data?.temporary) : temporaryChat
  const exitTemporary = () => {
    setTemporaryChat(false)
    if (conversationId) void navigate({ to: '/' })
  }

  const title = conversationId ? displayTitle(conversation.data?.title, t) : t('chat.nav.new')

  // Exactly one composer for every state; on the empty state it docks beneath
  // the centred welcome rather than living inside it.
  const composer = (
    <Composer
      value={input}
      onValueChange={setInput}
      onSend={() => void handleSend()}
      onStop={handleStop}
      note={revising ? t('chat.revise.label') : undefined}
      quotes={
        revising
          ? undefined
          : quotes.map((q, i) => ({ title: q.title, passage: q.passage, fromAnswer: !q.documentId, onRemove: () => quoteState.remove(i) }))
      }
      working={working}
      disabled={createConversation.isPending}
      modelSlot={
        <>
          {conversationId && (
            <ContextMeter conversationId={conversationId} limit={modelById.get(modelId)?.contextLimit} />
          )}
          {canThink && <ThinkingPicker levels={thinkingLevels} value={effort} onChange={pickEffort} />}
          <ModelPicker models={available} value={modelId} onChange={pickModel} />
        </>
      }
      toolsSlot={canSearch && <WebSearchToggle on={webSearch} onChange={pickWebSearch} />}
      attach={{
        accept: acceptAttribute(accepts),
        hint: uploadHint,
        items: attachments.items,
        onAdd: attachments.add,
        onRemove: attachments.remove,
        conversationId,
        references,
        onRefer: refer,
        onUnrefer: unrefer,
        blocked: attachments.uploading
          ? t('chat.file.wait')
          : attachments.failed
            ? t('chat.file.failed')
            : undefined,
      }}
    />
  )

  // 轨迹 is an administrator's, of their own saved conversations (api/traces.go).
  const traceable =
    Boolean(conversationId) && !temporaryMode && session?.role === 'admin' && features.data?.requestTraces === true
  const tracing = traceable && traceOpen
  // A writing block of this conversation open beside the chat.
  const panel = useDocumentPanel()
  const docOpen = Boolean(conversationId) && panel.conversationId === conversationId && panel.documentId !== null
  // Or its sub-agents, in the same place (one or the other).
  const subagentPanel = useSubagentPanel()
  const subagentsOn = Boolean(conversationId) && features.data?.subagents === true
  const agentsOpen = subagentsOn && subagentPanel.conversationId === conversationId
  // A text file someone sent, read beside the chat.
  const filePanel = useFilePanel()
  const fileOpen = Boolean(conversationId) && filePanel.conversationId === conversationId && filePanel.file !== null
  const beside = (docOpen && !panel.closing) || agentsOpen || fileOpen
  // How wide the person dragged the panel; null is its default width.
  const [panelWidth, setPanelWidth] = useState(loadPanelWidth)
  const resizePanel = useCallback((width: number | null) => {
    setPanelWidth(width)
    savePanelWidth(width)
  }, [])

  return (
    <main
      className={cn('n-main', isEmpty && 'is-welcome', temporaryMode && 'is-temporary', beside && 'has-doc', docOpen && !panel.closing && panel.full && 'is-doc-full')}
      style={panelWidth ? ({ '--n-doc-user-w': `${panelWidth}px` } as React.CSSProperties) : undefined}
      {...fileDrop.handlers}
    >
      <DropOverlay show={fileDrop.dragging} unavailable={canAttach ? undefined : uploadHint} />
      <div className={conversationId ? 'n-topbar has-chat' : 'n-topbar'}>
        <SidebarToggle />
        {/* A temporary chat is never titled; its band says what it is. */}
        {conversationId && !temporaryMode && <div className="n-chat-title">{title}</div>}
        {traceable && (
          <div className="n-view-switch" role="tablist" aria-label={t('trace.switch')}>
            <button type="button" role="tab" aria-selected={!tracing} className={cn('n-view-tab', !tracing && 'active')} onClick={() => setTraceOpen(false)}>
              {t('trace.conversation')}
            </button>
            <button type="button" role="tab" aria-selected={tracing} className={cn('n-view-tab', tracing && 'active')} onClick={() => setTraceOpen(true)}>
              {t('trace.title')}
            </button>
          </div>
        )}
        <div className="n-topbar-end">
          {!temporaryMode && !conversationId && <TemporaryToggle />}
          {subagentsOn && conversationId && !tracing && <SubagentsButton conversationId={conversationId} />}
          {conversationId && !tracing && <DocumentsMenu conversationId={conversationId} />}
        </div>
      </div>
      {tracing && conversationId && <TraceView conversationId={conversationId} running={isRunning(live)} />}
      {docOpen && panel.documentId && <DocumentPanel key={panel.documentId} documentId={panel.documentId} />}
      {agentsOpen && conversationId && <SubagentPanel conversationId={conversationId} />}
      {fileOpen && filePanel.file && <FilePanel key={filePanel.file.id} file={filePanel.file} />}
      {conversationId && !revising && (
        <AskSelection
          scope={columnRef}
          onAsk={(passage) => {
            quoteState.add({ ...answerQuote(passage), conversationId })
            dock.current?.querySelector<HTMLTextAreaElement>('textarea')?.focus()
          }}
        />
      )}
      {beside && !(docOpen && panel.full) && <PanelResizer width={panelWidth} onChange={resizePanel} />}

      <div className={isEmpty ? 'n-scroll n-scroll-welcome' : 'n-scroll'} ref={scrollRef} onScroll={handleScroll}>
        {conversationId && messages.isPending && !outgoing ? (
          <div className="n-loading" role="status" aria-label={t('state.loading')}>
            <span className="n-spinner" data-motion="essential" />
          </div>
        ) : isEmpty ? (
          <Welcome />
        ) : (
          <ImageSource.Provider value={images}>
            <div className={attachments.items.length > 0 ? 'n-column n-chat-fade has-files' : 'n-column n-chat-fade'} ref={columnRef}>
              {(start > floor || hasOlder) && (
                // Room for the spinner is always kept while there is more above,
                // so it appearing does not move the transcript.
                <div className="flex h-10 items-center justify-center" data-edge>
                  {messages.isFetchingNextPage && (
                    <span className="n-spinner" data-motion="essential" role="status" aria-label={t('state.loading')} />
                  )}
                </div>
              )}
              {turns.slice(start).map((turn) => (
                <Fragment key={turn.key}>
                  {turn.kind === 'user' ? (
                    <UserMessage
                      message={turn.message}
                      anchor={turn.key}
                      time={
                        gaps.has(turn.key)
                          ? gapLabel(gaps.get(turn.key)!, Math.max(openedAt, gaps.get(turn.key)!), i18n.resolvedLanguage ?? 'zh', {
                              today: t('chat.time-gap.today'),
                              yesterday: t('chat.time-gap.yesterday'),
                            })
                          : undefined
                      }
                      // The refused message that ends the conversation is edited in the composer.
                      onEdit={!working && !outgoing && turn.message.id !== revising?.id ? (content) => editQuestion(turn.message, content) : undefined}
                      restoredDraft={refusedEdit?.id === turn.message.id ? refusedEdit.content : undefined}
                      onDraftDone={() => setRefusedEdit(null)}
                    />
                  ) : turn.kind === 'reports' ? (
                    // Sub-agents' reports, delivered while the conversation was idle:
                    // a divider each, and the reply they started below.
                    <div className="n-msg-notice">
                      <SubagentReportLines conversationId={turn.message.conversationId} notices={turn.notices} divider />
                    </div>
                  ) : turn.kind === 'refusal' ? (
                    // The reply to a message moderation refused, where the model's would be.
                    <RefusedReply modelId={modelId} model={modelById.get(modelId)} refusal={refusals[turn.message.id] ?? {}} />
                  ) : (
                    <ChoicesContext
                      value={
                        turn === last?.answer && canPick
                          ? { next: sentAfter.get(turn.key)?.next, pick: (message) => void handlePick(message) }
                          : (sentAfter.get(turn.key) ?? NO_CHOICES)
                      }
                    >
                    <AssistantTurn
                      turn={turn}
                      modelId={turnModel(turn) ?? modelId}
                      model={modelById.get(turnModel(turn) ?? modelId)}
                      askingCallId={askingCallId}
                      onOpenQuestion={() => setDismissedCallId(null)}
                      onRegenerate={turn === last?.answer && !working && !outgoing ? handleRegenerate : undefined}
                      divider={divider}
                    />
                    </ChoicesContext>
                  )}
                  {/* Where the compressed context ends. The messages above are all still here. */}
                  {turn.after && <CompactionDivider summary={turn.after} {...divider} />}
                </Fragment>
              ))}
              {replacing && !outgoing && <PendingReply modelId={modelId} model={modelById.get(modelId)} />}
              {outgoing && (
                <>
                  <UserMessage
                    message={{
                      id: 'outbox',
                      conversationId: conversationId ?? '',
                      role: 'user',
                      content: outgoing.content,
                      parts: [],
                      status: 'complete',
                      createdAt: '',
                      attachments: outgoing.attachments,
                      references: outgoing.references,
                    }}
                  />
                  <PendingReply modelId={modelId} model={modelById.get(modelId)} />
                </>
              )}
            </div>
          </ImageSource.Provider>
        )}
      </div>

      {conversationId && !isEmpty && !tracing && !beside && <TurnNavigator entries={outline} scrollRef={scrollRef} />}

      <div ref={dock} className="n-composer-dock">
        <div className="n-column relative">
          {asking && (
            <AskUserDock
              key={asking.callId}
              questions={asking.questions}
              busy={answerQuestion.isPending}
              hidden={dismissedCallId === asking.callId}
              onSubmit={(answers) => handleAnswer(asking.callId, answers)}
              onDismiss={dismissQuestion}
            />
          )}
          {showJump && !isEmpty && (
            <button
              type="button"
              className="n-scroll-bottom"
              aria-label={t('chat.scroll-bottom')}
              onClick={() => scrollToBottom()}
            >
              <ArrowDown className="h-4 w-4" />
            </button>
          )}
          {subagentsOn && conversationId && <SubagentSummary conversationId={conversationId} />}
          {composer}
          {isEmpty && !temporaryMode && <WelcomeStarters onPick={setInput} />}
        </div>
      </div>
      {/* Last, so turning it on or off never shifts the elements above it. */}
      {temporaryMode && <TemporaryBar onExit={exitTemporary} />}
    </main>
  )
}

/** How long the composer takes to glide from the new-chat page's middle to the foot. */
const DOCK_GLIDE_MS = 420

/** The model a reply was answered with: its run's, or its rows'. */
function turnModel(turn: Extract<ReturnType<typeof buildTurns>[number], { kind: 'assistant' }>) {
  return turn.live?.run.model ?? turn.responses.find((m) => m.model)?.model
}

export default ChatView
