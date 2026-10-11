import { isOpenChatError, type Attachment, type ReasoningEffort } from '@openchat/api-client'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { api, unwrap } from '@/lib/api'
import type { AskAnswer } from '@/lib/ask-user'
import { CAPACITY_CODE } from '@/lib/model-capacity'
import { queryKeys } from '@/lib/query-keys'
import { truncateTranscript, type TranscriptPages } from '@/lib/transcript-pages'
import { useChatStore } from '@/stores/chat-store'

/** Server error codes a send can fail with that have their own wording. */
const SEND_ERRORS = [
  'conversation_busy',
  'no_models',
  'model_unavailable',
  'model_capacity_reached',
  'content_blocked',
  'moderation_unavailable',
  'too_many_runs',
  'too_many_attachments',
  'attachments_too_large',
  'attachment_unsupported',
  'attachment_unavailable',
  'nothing_to_answer',
  'not_latest',
  'message_not_found',
  'not_pending',
  'invalid_answers',
  'guest_ip_limit',
  'guest_daily_limit',
  'guest_unreachable',
] as const

function useSendError() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  return (error: Error) => {
    const code = isOpenChatError(error) ? error.code : ''
    const known = SEND_ERRORS.find((c) => c === code)
    // The request's id under the message, for a report that finds its lines.
    const requestId = isOpenChatError(error) ? error.requestId : undefined
    toast.error(known ? t(`chat.send-error.${known}`) : t('chat.send-error.generic'), {
      description: requestId ? t('chat.error-id', { id: requestId }) : undefined,
    })
    // The picker learned of the cap too late: its list says the model has room.
    // Fetch it again so the model shows as unavailable, not as a trap.
    if (code === CAPACITY_CODE) void queryClient.invalidateQueries({ queryKey: queryKeys.models.all })
  }
}

/** What a run is started with, as the composer has it set. */
interface RunChoice {
  model?: string
  reasoningEffort?: ReasoningEffort
  webSearch?: boolean
}

const choiceBody = ({ model, reasoningEffort, webSearch }: RunChoice) => ({
  ...(model ? { model } : {}),
  ...(reasoningEffort ? { reasoningEffort } : {}),
  ...(webSearch !== undefined ? { webSearch } : {}),
})

/**
 * Sends a message. The server writes it, starts the run that answers it and
 * returns at once; the answer arrives on the conversation's events stream, so
 * nothing here waits for it — and closing the page does not stop it.
 */
export function useSendMessage(conversationId: string) {
  const startRun = useChatStore((s) => s.startRun)
  const unpost = useChatStore((s) => s.unpost)
  const onError = useSendError()

  return useMutation({
    // Shown in the transcript itself (the outbox, a waiting reply).
    meta: { silent: true },
    mutationFn: async ({
      content,
      attachmentIds,
      ...choice
    }: RunChoice & {
      content: string
      /** Uploads (see `useAttachments`) to send with it. */
      attachmentIds?: string[]
    }) =>
      unwrap(
        await api.POST('/conversations/{conversationId}/messages', {
          params: { path: { conversationId } },
          // A retried request with the same id finds the run it started rather
          // than paying for a second answer.
          body: {
            content,
            requestId: crypto.randomUUID(),
            ...choiceBody(choice),
            ...(attachmentIds?.length ? { attachmentIds } : {}),
          },
        }),
      ),
    onSuccess: (response) => startRun(conversationId, response.run, response.message),
    // Here and not in the call's own `onError`: that one is skipped when the
    // page that made the call is gone, and a message sent just before switching
    // conversations would stay in the transcript as a bubble with a reply that
    // never comes. (The same goes for the prompt carried over from the empty screen.)
    onError: (error) => {
      unpost(conversationId)
      onError(error)
    },
  })
}

/**
 * Takes the rows a redo deleted out of the cached transcript, for the moment
 * between the server having done it and the fetch that shows the same. The
 * events stream gets there first when it can (`use-conversation-events.ts`).
 */
export function useForgetDeleted(conversationId: string) {
  const queryClient = useQueryClient()
  return (target: { from?: string; after?: string }) =>
    queryClient.setQueryData<TranscriptPages>(queryKeys.messages.list(conversationId), (held) =>
      truncateTranscript(held, target),
    )
}

/**
 * Answers the last message again, with the model and settings the composer
 * has now. The answer it replaces is deleted, not kept. Until the server has
 * taken it, the old answer is hidden and a waiting reply shown in its place,
 * the way a sent message shows at once.
 */
export function useRegenerate(conversationId: string) {
  const startRun = useChatStore((s) => s.startRun)
  const redo = useChatStore((s) => s.redo)
  const forget = useForgetDeleted(conversationId)
  const onError = useSendError()

  return useMutation({
    // Shown in the transcript itself (the outbox, a waiting reply).
    meta: { silent: true },
    mutationFn: async ({ inputId: _, ...choice }: RunChoice & { inputId: string }) =>
      unwrap(
        await api.POST('/conversations/{conversationId}/regenerate', {
          params: { path: { conversationId } },
          body: { requestId: crypto.randomUUID(), ...choiceBody(choice) },
        }),
      ),
    // The answer is the rows after the question it answers.
    onMutate: ({ inputId }) => redo(conversationId, { after: inputId }),
    onSuccess: (response) => {
      forget({ after: response.message.id })
      startRun(conversationId, response.run, response.message)
    },
    onError: (error) => {
      redo(conversationId, null)
      onError(error)
    },
  })
}

/**
 * Asks a question again in new words — any question in the conversation. The
 * old wording and everything after it are deleted, and the new wording goes
 * on the end. Until the server has taken it, the old wording and the turns
 * after it are hidden and the new wording shown with a waiting reply, the way
 * a sent message shows at once.
 */
export function useEditMessage(conversationId: string) {
  const startRun = useChatStore((s) => s.startRun)
  const redo = useChatStore((s) => s.redo)
  const post = useChatStore((s) => s.post)
  const unpost = useChatStore((s) => s.unpost)
  const forget = useForgetDeleted(conversationId)
  const onError = useSendError()

  return useMutation({
    // Shown in the transcript itself (the outbox, a waiting reply).
    meta: { silent: true },
    mutationFn: async ({
      messageId,
      content,
      attachments: _attachments,
      ...choice
    }: RunChoice & {
      messageId: string
      content: string
      /** The question's files, which the edit keeps. */
      attachments: Attachment[]
    }) =>
      unwrap(
        await api.POST('/conversations/{conversationId}/messages/{messageId}/edit', {
          params: { path: { conversationId, messageId } },
          body: { content, requestId: crypto.randomUUID(), ...choiceBody(choice) },
        }),
      ),
    onMutate: ({ messageId, content, attachments }) => {
      redo(conversationId, { from: messageId })
      post(conversationId, { content, attachments })
    },
    onSuccess: (response, { messageId }) => {
      forget({ from: messageId })
      startRun(conversationId, response.run, response.message)
    },
    onError: (error) => {
      redo(conversationId, null)
      unpost(conversationId)
      onError(error)
    },
  })
}

/**
 * Answers the question the model asked. The server writes the answers as the
 * call's result and starts the run that goes on from it — part of the same
 * reply — and returns at once; the rest arrives on the events stream. Nothing
 * waits for the user's answer on the server: the question was a finished run.
 */
export function useAnswerQuestion(conversationId: string) {
  const startRun = useChatStore((s) => s.startRun)
  const onError = useSendError()

  return useMutation({
    // Shown in the question's card itself.
    meta: { silent: true },
    mutationFn: async ({ callId, answers, ...choice }: RunChoice & { callId: string; answers: AskAnswer[] }) =>
      unwrap(
        await api.POST('/conversations/{conversationId}/tool-calls/{callId}/answer', {
          params: { path: { conversationId, callId } },
          body: { answers, requestId: crypto.randomUUID(), ...choiceBody(choice) },
        }),
      ),
    onSuccess: (response) => startRun(conversationId, response.run, response.message, [response.result]),
    onError,
  })
}

/** Stops a run. What it generated so far is kept. */
export function useCancelRun(conversationId: string) {
  return useMutation({
    // Shown in the transcript itself (the waiting reply).
    meta: { silent: true },
    mutationFn: async (runId: string) =>
      unwrap(
        await api.POST('/conversations/{conversationId}/runs/{runId}/cancel', {
          params: { path: { conversationId, runId } },
        }),
      ),
  })
}
