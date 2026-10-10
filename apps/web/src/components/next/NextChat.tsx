import { useNavigate } from '@tanstack/react-router'
import { ArrowDown } from 'lucide-react'
import { useCallback, useEffect, useEffectEvent, useLayoutEffect, useMemo, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import type { Message, ReasoningEffort } from '@openchat/api-client'
import { useChatFeatures, useChatModels, useUpdateConversationSettings } from '@/hooks/use-chat-models'
import { useConversationEvents } from '@/hooks/use-conversation-events'
import { useConversation, useCreateConversation } from '@/hooks/use-conversations'
import { useAttachments } from '@/hooks/use-attachments'
import { useFileDrop } from '@/hooks/use-file-drop'
import { useMessages } from '@/hooks/use-messages'
import {
  useAnswerQuestion,
  useCancelRun,
  useChooseAnswer,
  useEditMessage,
  useRegenerate,
  useSendMessage,
  useSwitchVersion,
} from '@/hooks/use-send-message'
import { useSession } from '@/features/auth/use-auth'
import { API_BASE_URL } from '@/lib/api'
import { loadComposerPrefs, saveComposerPrefs } from '@/lib/composer-prefs'
import { acceptAttribute, acceptsOf } from '@/lib/attachments'
import { isRunning } from '@/lib/run-state'
import { buildTurns, pendingQuestion } from '@/lib/transcript'
import { parseQuestions, type AskAnswer } from '@/lib/ask-user'
import { useChatStore } from '@/stores/chat-store'
import AskUserDock from './AskUserDock'
import Composer from './Composer'
import ContextMeter from './ContextMeter'
import { DropOverlay } from './DropOverlay'
import { ImageSource, proxiedImages } from './genui/image-source'
import { AssistantTurn, PendingReply, UserMessage } from './MessageItem'
import ModelPicker from './ModelPicker'
import ThinkingPicker from './ThinkingPicker'
import WebSearchToggle from './WebSearchToggle'
import { SidebarToggle } from './NextShell'
import { TemporaryBar, TemporaryToggle } from './TemporaryChat'
import Welcome from './Welcome'

/** Distance from the bottom, in px, still counted as "parked at the tail". */
const BOTTOM_SLACK = 24
/** Turns drawn when a conversation opens, and drawn more of each time the reader nears the top. */
const TURN_STEP = 20
/** How close to the top, in px, the reader must be before older turns are drawn (or loaded). */
const TOP_REACH = 800

interface NextChatProps {
  conversationId?: string
}

export function NextChat({ conversationId }: NextChatProps) {
  const { t } = useTranslation()
  const navigate = useNavigate()

  const [input, setInput] = useState('')
  const scrollRef = useRef<HTMLDivElement>(null)
  const columnRef = useRef<HTMLDivElement>(null)
  const parked = useRef(true)
  const [showJump, setShowJump] = useState(false)

  const conversation = useConversation(conversationId ?? '')
  const messages = useMessages(conversationId ?? '')
  const models = useChatModels()
  const createConversation = useCreateConversation()
  const sendMessage = useSendMessage(conversationId ?? '')
  const cancelRun = useCancelRun(conversationId ?? '')
  const regenerate = useRegenerate(conversationId ?? '')
  const chooseAnswer = useChooseAnswer(conversationId ?? '')
  const editMessage = useEditMessage(conversationId ?? '')
  const switchVersion = useSwitchVersion(conversationId ?? '')
  const answerQuestion = useAnswerQuestion(conversationId ?? '')
  const updateSettings = useUpdateConversationSettings()

  // Attaches to whatever this conversation is running — including a run that
  // kept going while the page was closed — and follows it.
  useConversationEvents(conversationId)

  const live = useChatStore((s) => (conversationId ? s.live[conversationId] : undefined))
  const setPendingPrompt = useChatStore((s) => s.setPendingPrompt)
  const outgoing = useChatStore((s) => s.outbox[conversationId ?? ''])
  const post = useChatStore((s) => s.post)
  const unpost = useChatStore((s) => s.unpost)
  const takePendingPrompt = useChatStore((s) => s.takePendingPrompt)
  const temporaryChat = useChatStore((s) => s.temporaryChat)
  const setTemporaryChat = useChatStore((s) => s.setTemporaryChat)
  const replacing = useChatStore((s) => s.replacing[conversationId ?? ''])

  // Anything that makes the model's open question moot, as writing something
  // else does, or is about to: a run going, a message or a redo on its way.
  const superseded =
    isRunning(live) || sendMessage.isPending || regenerate.isPending || editMessage.isPending || Boolean(outgoing) || Boolean(replacing)
  const working = superseded || answerQuestion.isPending

  // What the composer was last set to, anywhere, for a new chat to open with.
  const userId = useSession().data?.id
  const saved = useMemo(() => loadComposerPrefs(userId), [userId])

  // The model: what this conversation remembers, else what was picked on the
  // empty screen, else the last one picked, else the first one offered — each
  // only while it is still offered, since a model an administrator has taken
  // away (or kept for administrators) would only be refused. Until the list
  // arrives the conversation's own is trusted, so the picker does not flicker.
  const available = useMemo(() => models.data ?? [], [models.data])
  const [draftModel, setDraftModel] = useState<string>()
  const modelById = useMemo(() => new Map(available.map((m) => [m.id, m])), [available])
  const offered = (id: string | null | undefined) =>
    id && (models.data === undefined || modelById.has(id)) ? id : undefined
  const lastModel = saved.model && modelById.has(saved.model) ? saved.model : undefined
  const modelId =
    offered(conversation.data?.model) ?? offered(draftModel) ?? lastModel ?? available[0]?.id ?? ''

  // Files: only what the selected model takes. With none that does, the button
  // and the drop zone say so rather than going quiet.
  const accepts = useMemo(() => acceptsOf(modelById.get(modelId)), [modelById, modelId])
  const anyModelTakesFiles = available.some((m) => acceptsOf(m).image || acceptsOf(m).pdf)
  const attachments = useAttachments(accepts)
  const uploadHint = accepts.image
    ? accepts.pdf
      ? t('next.file.upload-both')
      : t('next.file.upload-image')
    : accepts.pdf
      ? t('next.file.upload-pdf')
      : anyModelTakesFiles
        ? t('next.file.unsupported')
        : t('next.file.none')
  const canAttach = accepts.image || accepts.pdf
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

  // Pictures in answers come through the server, for this conversation only.
  const images = useMemo(
    () => (conversationId ? proxiedImages(API_BASE_URL, conversationId) : () => null),
    [conversationId],
  )

  const turns = useMemo(
    () => buildTurns(messages.data?.items ?? [], live?.run ?? null, replacing),
    [messages.data, live, replacing],
  )

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
  const anchor = useRef<{ el: HTMLElement; top: number } | null>(null)

  // The last answer can be asked for again, and its versions stepped through:
  // only the last, as an earlier one has later turns built on it.
  const last = useMemo(() => {
    const answer = turns.at(-1)
    const input = [...turns].reverse().find((turn) => turn.kind === 'user')
    if (answer?.kind !== 'assistant' || input?.kind !== 'user') return null
    const runs = input.message.versions ?? []
    return { answer, inputId: input.message.id, runs, at: runs.indexOf(answer.key) }
  }, [turns])

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
      replacing: last.answer.runs,
      model: modelId || undefined,
      reasoningEffort: canThink ? effort : undefined,
      webSearch: canSearch ? webSearch : undefined,
    })
  }

  // Any question can be reworded and asked again: the new wording starts a
  // branch, and the turns after the old one leave the transcript (they stay on
  // the server, behind the question's version pager).
  const editQuestion = (message: Message, content: string) => {
    parked.current = true
    editMessage.mutate({
      messageId: message.id,
      content,
      attachments: message.attachments ?? [],
      model: modelId || undefined,
      reasoningEffort: canThink ? effort : undefined,
      webSearch: canSearch ? webSearch : undefined,
    })
  }

  // The versions of a question: stepping to one shows it with the turns that
  // followed it, as that branch was left.
  const pickQuestion = (message: Message, index: number) => {
    const messageId = message.branches?.[index]
    if (!messageId) return
    parked.current = true
    switchVersion.mutate({ messageId })
  }

  const pickAnswer = (index: number) => {
    const runId = last?.runs[index]
    if (runId) chooseAnswer.mutate({ runId, inputId: last.inputId })
  }

  const isEmpty = !outgoing && (!conversationId || (!messages.isPending && turns.length === 0))

  // Grows as the reply streams, so the tail-follow below reacts to tokens and
  // not only to new rows.
  const streamed = (live?.run?.partial ?? []).reduce(
    (n, part) => n + (part.type === 'tool_call' ? part.rawArguments.length : 'text' in part ? part.text.length : 0),
    0,
  )

  const scrollToBottom = useCallback((behavior: ScrollBehavior = 'smooth') => {
    const el = scrollRef.current
    if (!el) return
    el.scrollTo({ top: el.scrollHeight, behavior })
    parked.current = true
    setShowJump(false)
  }, [])

  const handleScroll = () => {
    const el = scrollRef.current
    if (!el) return
    const distance = el.scrollHeight - el.scrollTop - el.clientHeight
    parked.current = distance <= BOTTOM_SLACK
    setShowJump(!parked.current)
    loadOlder()
  }

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

  const handleSend = useCallback(async () => {
    const content = input.trim()
    if (!content || attachments.uploading || attachments.failed) return
    const attachmentIds = attachments.ids
    setInput('')
    // Shown at once, at the bottom, while the request is on its way.
    const item = {
      content,
      attachments: attachments.items
        .filter((file) => file.id)
        .map((file) => ({
          id: file.id!,
          filename: file.name,
          contentType: file.kind === 'image' ? 'image/*' : 'application/pdf',
          size: file.size,
          url: file.preview ?? '',
        })),
    }
    parked.current = true

    if (conversationId) {
      post(conversationId, item)
      sendMessage.mutate(
        {
          content,
          model: modelId || undefined,
          reasoningEffort: canThink ? effort : undefined,
          webSearch: canSearch ? webSearch : undefined,
          attachmentIds,
        },
        {
          // The files stay in the composer until the server has taken them, and
          // a refused message comes back as it was written.
          onSuccess: () => attachments.clear(),
          onError: () => {
            unpost(conversationId)
            setInput((current) => current || content)
          },
        },
      )
      return
    }

    // No conversation yet: create one with the chosen model, hand the prompt
    // and its uploads to the next route, and let it send once it owns the id.
    setPendingPrompt({ content, attachmentIds })
    post('', item)
    try {
      const created = await createConversation.mutateAsync({
        ...(modelId ? { model: modelId } : {}),
        ...(canThink && effort !== 'auto' ? { reasoningEffort: effort } : {}),
        ...(canSearch && !webSearch ? { webSearch: false } : {}),
        ...(temporaryChat ? { temporary: true } : {}),
      })
      // Once made, the conversation itself says it is temporary; the switch
      // starts the next new chat off again.
      setTemporaryChat(false)
      // The message moves with it to the conversation's own page.
      post(created.id, item)
      unpost('')
      await navigate({ to: '/chat/$conversationId', params: { conversationId: created.id } })
    } catch {
      // Nothing was sent: the text comes back, and the files were never let go.
      unpost('')
      setPendingPrompt(null)
      setInput((current) => current || content)
      toast.error(t('chat.send-error.generic'))
      return
    }
    attachments.clear()
  }, [
    attachments,
    canSearch,
    canThink,
    conversationId,
    createConversation,
    effort,
    input,
    modelId,
    navigate,
    post,
    sendMessage,
    setPendingPrompt,
    setTemporaryChat,
    t,
    temporaryChat,
    unpost,
    webSearch,
  ])

  const handleStop = useCallback(() => {
    const runId = live?.run?.run.id
    if (runId) cancelRun.mutate(runId)
  }, [cancelRun, live])

  // Consume a prompt carried over from the empty state.
  useEffect(() => {
    if (!conversationId) return
    const pending = takePendingPrompt()
    if (pending) {
      sendMessage.mutate(pending, {
        onError: () => {
          unpost(conversationId)
          setInput((current) => current || pending.content)
        },
      })
    }
    // sendMessage is a fresh object each render; the prompt is taken once.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [conversationId, takePendingPrompt])

  // A temporary chat, made or about to be: the chat is drawn in its dark frame.
  const temporaryMode = conversationId ? Boolean(conversation.data?.temporary) : temporaryChat
  const exitTemporary = () => {
    setTemporaryChat(false)
    if (conversationId) void navigate({ to: '/' })
  }

  const title = conversationId ? (conversation.data?.title ?? '') : t('next.nav.new')
  const modelName = modelById.get(modelId)?.name ?? (modelId || 'OpenChat')

  // Exactly one composer for every state; on the empty state it docks beneath
  // the centred welcome rather than living inside it.
  const composer = (
    <Composer
      value={input}
      onValueChange={setInput}
      onSend={() => void handleSend()}
      onStop={handleStop}
      working={working}
      disabled={createConversation.isPending}
      modelSlot={
        <>
          {conversationId && <ContextMeter conversationId={conversationId} limit={modelById.get(modelId)?.contextLimit} />}
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
        blocked: attachments.uploading
          ? t('next.file.wait')
          : attachments.failed
            ? t('next.file.failed')
            : undefined,
      }}
    />
  )

  return (
    <main className={temporaryMode ? 'n-main is-temporary' : 'n-main'} {...fileDrop.handlers}>
      <DropOverlay show={fileDrop.dragging} unavailable={canAttach ? undefined : uploadHint} />
      <div className={conversationId ? 'n-topbar has-chat' : 'n-topbar'}>
        <SidebarToggle />
        {/* A temporary chat is never titled; its band says what it is. */}
        {conversationId && !temporaryMode && <div className="n-chat-title">{title}</div>}
        <div className="n-topbar-end">
          {!temporaryMode && !conversationId && <TemporaryToggle />}
        </div>
      </div>

      <div className={isEmpty ? 'n-scroll n-scroll-welcome' : 'n-scroll'} ref={scrollRef} onScroll={handleScroll}>
        {conversationId && messages.isPending && !outgoing ? (
          <div className="n-loading" role="status" aria-label={t('state.loading')}>
            <span className="n-spinner" data-motion="essential" />
          </div>
        ) : isEmpty ? (
          <Welcome onPick={setInput} modelName={modelName} model={modelById.get(modelId)} />
        ) : (
          <ImageSource.Provider value={images}>
            <div className="n-column n-chat-fade" ref={columnRef}>
              {(start > floor || hasOlder) && (
                // Room for the spinner is always kept while there is more above,
                // so it appearing does not move the transcript.
                <div className="flex h-10 items-center justify-center" data-edge>
                  {messages.isFetchingNextPage && (
                    <span className="n-spinner" data-motion="essential" role="status" aria-label={t('state.loading')} />
                  )}
                </div>
              )}
              {turns.slice(start).map((turn) => {
                const pager = turn.kind === 'user' ? versionsOf(turn.message) : undefined
                return turn.kind === 'user' ? (
                  <UserMessage
                    key={turn.key}
                    message={turn.message}
                    onEdit={!working && !outgoing ? (content) => editQuestion(turn.message, content) : undefined}
                    versions={
                      pager && {
                        ...pager,
                        busy: working || switchVersion.isPending,
                        onPick: (to) => pickQuestion(turn.message, to),
                      }
                    }
                  />
                ) : (
                  <AssistantTurn
                    key={turn.key}
                    turn={turn}
                    modelId={turnModel(turn) ?? modelId}
                    model={modelById.get(turnModel(turn) ?? modelId)}
                    askingCallId={askingCallId}
                    onOpenQuestion={() => setDismissedCallId(null)}
                    onRegenerate={turn === last?.answer && !working && !outgoing ? handleRegenerate : undefined}
                    versions={
                      turn === last?.answer && last.at >= 0 && last.runs.length > 1
                        ? { index: last.at, count: last.runs.length, onPick: pickAnswer, busy: working || chooseAnswer.isPending }
                        : undefined
                    }
                  />
                )
              })}
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
                    }}
                  />
                  <PendingReply modelId={modelId} model={modelById.get(modelId)} />
                </>
              )}
            </div>
          </ImageSource.Provider>
        )}
      </div>

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
              aria-label={t('next.scroll-bottom')}
              onClick={() => scrollToBottom()}
            >
              <ArrowDown className="h-4 w-4" />
            </button>
          )}
          {composer}
        </div>
      </div>
      {/* Last, so turning it on or off never shifts the elements above it. */}
      {temporaryMode && <TemporaryBar onExit={exitTemporary} />}
    </main>
  )
}

/** Where a question is among its versions, if it has others. */
function versionsOf(message: Message) {
  const branches = message.branches
  const index = branches?.indexOf(message.id) ?? -1
  return branches && branches.length > 1 && index >= 0 ? { index, count: branches.length } : undefined
}

/** The model a reply was answered with: its run's, or its rows'. */
function turnModel(turn: Extract<ReturnType<typeof buildTurns>[number], { kind: 'assistant' }>) {
  return turn.live?.run.model ?? turn.responses.find((m) => m.model)?.model
}

export default NextChat
