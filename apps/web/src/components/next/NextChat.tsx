import { useNavigate } from '@tanstack/react-router'
import { ArrowDown } from 'lucide-react'
import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import type { ReasoningEffort } from '@openchat/api-client'
import { useChatFeatures, useChatModels, useUpdateConversationSettings } from '@/hooks/use-chat-models'
import { useConversationEvents } from '@/hooks/use-conversation-events'
import { useConversation, useCreateConversation } from '@/hooks/use-conversations'
import { useAttachments } from '@/hooks/use-attachments'
import { useFileDrop } from '@/hooks/use-file-drop'
import { useMessages } from '@/hooks/use-messages'
import { useCancelRun, useSendMessage } from '@/hooks/use-send-message'
import { acceptAttribute, acceptsOf } from '@/lib/attachments'
import { isRunning } from '@/lib/run-state'
import { buildTurns } from '@/lib/transcript'
import { useChatStore } from '@/stores/chat-store'
import Composer from './Composer'
import { DropOverlay } from './DropOverlay'
import { AssistantTurn, UserMessage } from './MessageItem'
import ModelPicker from './ModelPicker'
import ThinkingControl from './ThinkingControl'
import WebSearchToggle from './WebSearchToggle'
import { SidebarToggle } from './NextShell'
import Welcome from './Welcome'

/** Distance from the bottom, in px, still counted as "parked at the tail". */
const BOTTOM_SLACK = 24

interface NextChatProps {
  conversationId?: string
}

export function NextChat({ conversationId }: NextChatProps) {
  const { t } = useTranslation()
  const navigate = useNavigate()

  const [input, setInput] = useState('')
  const scrollRef = useRef<HTMLDivElement>(null)
  const parked = useRef(true)
  const [showJump, setShowJump] = useState(false)

  const conversation = useConversation(conversationId ?? '')
  const messages = useMessages(conversationId ?? '')
  const models = useChatModels()
  const createConversation = useCreateConversation()
  const sendMessage = useSendMessage(conversationId ?? '')
  const cancelRun = useCancelRun(conversationId ?? '')
  const updateSettings = useUpdateConversationSettings()

  // Attaches to whatever this conversation is running — including a run that
  // kept going while the page was closed — and follows it.
  useConversationEvents(conversationId)

  const live = useChatStore((s) => (conversationId ? s.live[conversationId] : undefined))
  const setPendingPrompt = useChatStore((s) => s.setPendingPrompt)
  const takePendingPrompt = useChatStore((s) => s.takePendingPrompt)

  const working = isRunning(live) || sendMessage.isPending

  // The model: what this conversation remembers, else what was picked on the
  // empty screen, else the first one offered.
  const available = useMemo(() => models.data ?? [], [models.data])
  const [draftModel, setDraftModel] = useState<string>()
  const modelId = conversation.data?.model ?? draftModel ?? available[0]?.id ?? ''
  const modelById = useMemo(() => new Map(available.map((m) => [m.id, m])), [available])

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
      if (conversationId) updateSettings.mutate({ conversationId, model: id })
    },
    [conversationId, updateSettings],
  )

  // Reasoning effort, remembered the same way. Offered only for a model the
  // catalogue says reasons; the server drops it for one it knows does not.
  const [draftEffort, setDraftEffort] = useState<ReasoningEffort>('auto')
  const effort = conversation.data?.reasoningEffort ?? draftEffort
  // The model's own thinking levels, from models.dev via the catalogue.
  const thinkingLevels = useMemo(() => modelById.get(modelId)?.reasoningEfforts ?? [], [modelById, modelId])
  const canThink = thinkingLevels.length > 0
  const pickEffort = useCallback(
    (next: ReasoningEffort) => {
      setDraftEffort(next)
      if (conversationId) updateSettings.mutate({ conversationId, reasoningEffort: next })
    },
    [conversationId, updateSettings],
  )

  // Web access, remembered the same way. Offered when the server has web
  // search on and the model may call tools; off, the run gets no web tools.
  const features = useChatFeatures()
  const [draftWebSearch, setDraftWebSearch] = useState(true)
  const webSearch = conversation.data?.webSearch ?? draftWebSearch
  const canSearch = (features.data?.webSearch ?? false) && (modelById.get(modelId)?.toolCall ?? true)
  const pickWebSearch = useCallback(
    (next: boolean) => {
      setDraftWebSearch(next)
      if (conversationId) updateSettings.mutate({ conversationId, webSearch: next })
    },
    [conversationId, updateSettings],
  )

  const turns = useMemo(
    () => buildTurns(messages.data?.items ?? [], live?.run ?? null),
    [messages.data, live],
  )
  const isEmpty = !conversationId || (!messages.isPending && turns.length === 0)

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

  const handleScroll = useCallback(() => {
    const el = scrollRef.current
    if (!el) return
    const distance = el.scrollHeight - el.scrollTop - el.clientHeight
    parked.current = distance <= BOTTOM_SLACK
    setShowJump(!parked.current)
  }, [])

  // Follow the tail while the reader has not scrolled away from it.
  useLayoutEffect(() => {
    // The landing page is its own scrollable document on short screens; opening
    // it at the bottom would hide the model heading and the input above it.
    if (!isEmpty && parked.current) scrollToBottom('auto')
  }, [isEmpty, turns.length, streamed, scrollToBottom])

  const handleSend = useCallback(async () => {
    const content = input.trim()
    if (!content || attachments.uploading || attachments.failed) return
    const attachmentIds = attachments.ids
    setInput('')

    if (conversationId) {
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
          onError: () => setInput((current) => current || content),
        },
      )
      return
    }

    // No conversation yet: create one with the chosen model, hand the prompt
    // and its uploads to the next route, and let it send once it owns the id.
    setPendingPrompt({ content, attachmentIds })
    try {
      const created = await createConversation.mutateAsync({
        ...(modelId ? { model: modelId } : {}),
        ...(canThink && effort !== 'auto' ? { reasoningEffort: effort } : {}),
        ...(canSearch && !webSearch ? { webSearch: false } : {}),
      })
      await navigate({ to: '/chat/$conversationId', params: { conversationId: created.id } })
    } catch {
      // Nothing was sent: the text comes back, and the files were never let go.
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
    sendMessage,
    setPendingPrompt,
    t,
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
      sendMessage.mutate(pending, { onError: () => setInput((current) => current || pending.content) })
    }
    // sendMessage is a fresh object each render; the prompt is taken once.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [conversationId, takePendingPrompt])

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
          {canThink && <ThinkingControl levels={thinkingLevels} value={effort} onChange={pickEffort} />}
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
    <main className="n-main" {...fileDrop.handlers}>
      <DropOverlay show={fileDrop.dragging} unavailable={canAttach ? undefined : uploadHint} />
      <div className="n-topbar">
        <SidebarToggle />
        {conversationId && <div className="n-chat-title">{title}</div>}
      </div>

      <div className={isEmpty ? 'n-scroll n-scroll-welcome' : 'n-scroll'} ref={scrollRef} onScroll={handleScroll}>
        {conversationId && messages.isPending ? (
          <div className="n-loading" role="status" aria-label={t('state.loading')}>
            <span className="n-spinner" />
          </div>
        ) : isEmpty ? (
          <Welcome onPick={setInput} modelName={modelName} model={modelById.get(modelId)} />
        ) : (
          <div className="n-column n-chat-fade">
            {turns.map((turn) =>
              turn.kind === 'user' ? (
                <UserMessage key={turn.key} message={turn.message} />
              ) : (
                <AssistantTurn
                  key={turn.key}
                  turn={turn}
                  modelId={turnModel(turn) ?? modelId}
                  model={modelById.get(turnModel(turn) ?? modelId)}
                />
              ),
            )}
          </div>
        )}
      </div>

      <div className="n-composer-dock">
        <div className="n-column relative">
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
    </main>
  )
}

/** The model a reply was answered with: its run's, or its rows'. */
function turnModel(turn: Extract<ReturnType<typeof buildTurns>[number], { kind: 'assistant' }>) {
  return turn.live?.run.model ?? turn.responses.find((m) => m.model)?.model
}

export default NextChat
