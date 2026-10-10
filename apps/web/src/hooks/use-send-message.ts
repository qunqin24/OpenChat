import { isOpenChatError, type Attachment, type ReasoningEffort } from '@openchat/api-client'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { PAGE_SIZE, spliceNewest, type TranscriptPages } from '@/hooks/use-messages'
import { api, unwrap } from '@/lib/api'
import type { AskAnswer } from '@/lib/ask-user'
import { queryKeys } from '@/lib/query-keys'
import { useChatStore } from '@/stores/chat-store'

/** Server error codes a send can fail with that have their own wording. */
const SEND_ERRORS = [
  'conversation_busy',
  'no_models',
  'model_unavailable',
  'too_many_attachments',
  'attachments_too_large',
  'attachment_unsupported',
  'attachment_unavailable',
  'nothing_to_answer',
  'not_latest',
  'not_on_path',
  'message_not_found',
  'not_pending',
  'invalid_answers',
  'guest_ip_limit',
  'guest_daily_limit',
  'guest_unreachable',
] as const

function useSendError() {
  const { t } = useTranslation()
  return (error: Error) => {
    const code = isOpenChatError(error) ? error.code : ''
    const known = SEND_ERRORS.find((c) => c === code)
    toast.error(known ? t(`chat.send-error.${known}`) : t('chat.send-error.generic'))
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
  const onError = useSendError()

  return useMutation({
    // Shown in the transcript itself (the outbox, a waiting reply, the pager).
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
    onError,
  })
}

/**
 * Answers the last message again, with the model and settings the composer
 * has now. The answer it replaces is kept as a version to switch back to.
 * Until the server has taken it, the old answer is hidden and a waiting reply
 * shown in its place, the way a sent message shows at once.
 */
export function useRegenerate(conversationId: string) {
  const startRun = useChatStore((s) => s.startRun)
  const redo = useChatStore((s) => s.redo)
  const onError = useSendError()

  return useMutation({
    // Shown in the transcript itself (the outbox, a waiting reply, the pager).
    meta: { silent: true },
    mutationFn: async ({ replacing: _, ...choice }: RunChoice & { replacing: string[] }) =>
      unwrap(
        await api.POST('/conversations/{conversationId}/regenerate', {
          params: { path: { conversationId } },
          body: { requestId: crypto.randomUUID(), ...choiceBody(choice) },
        }),
      ),
    onMutate: ({ replacing }) => redo(conversationId, { runs: replacing }),
    onSuccess: (response) => startRun(conversationId, response.run, response.message),
    onError: (error) => {
      redo(conversationId, null)
      onError(error)
    },
  })
}

/**
 * Asks a question again in new words — any question on the branch shown. The
 * new wording starts a branch beside the old one, which is kept (with what
 * followed it) to switch back to. Until the server has taken it, the old
 * wording and the turns after it are hidden and the new wording shown with a
 * waiting reply, the way a sent message shows at once.
 */
export function useEditMessage(conversationId: string) {
  const startRun = useChatStore((s) => s.startRun)
  const redo = useChatStore((s) => s.redo)
  const post = useChatStore((s) => s.post)
  const unpost = useChatStore((s) => s.unpost)
  const onError = useSendError()

  return useMutation({
    // Shown in the transcript itself (the outbox, a waiting reply, the pager).
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
    onSuccess: (response) => startRun(conversationId, response.run, response.message),
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

/**
 * Shows another answer to the last question. The server answers with the
 * question and that answer's rows, which take the place of the turn in the
 * cached transcript: no refetch, so the switch costs one round trip.
 */
export function useChooseAnswer(conversationId: string) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()

  return useMutation({
    // Shown in the transcript itself (the outbox, a waiting reply, the pager).
    meta: { silent: true },
    mutationFn: async ({ runId }: { runId: string; inputId: string }) =>
      unwrap(
        await api.POST('/conversations/{conversationId}/runs/{runId}/choose', {
          params: { path: { conversationId, runId } },
        }),
      ),
    onSuccess: (chosen, { inputId }) => {
      // A finished run still held live would put its own rows back.
      const held = useChatStore.getState().live[conversationId]?.run?.run.id
      if (held) useChatStore.getState().settle(conversationId, held)
      queryClient.setQueryData<TranscriptPages>(queryKeys.messages.list(conversationId), (held) => {
        if (!held) return held
        // The turn is the transcript's last, so its input is in the newest
        // page — unless the turn straddles a page edge, in which case the page
        // it starts in becomes the newest.
        const at = held.pages.findIndex((p) => p.items.some((m) => m.id === inputId))
        const page = held.pages[at]
        if (!page) return held
        const items = [...page.items.slice(0, page.items.findIndex((m) => m.id === inputId)), ...chosen.items]
        return {
          pages: [{ ...page, items }, ...held.pages.slice(at + 1)],
          pageParams: [undefined, ...held.pageParams.slice(at + 1)],
        }
      })
    },
    onError: () => toast.error(t('chat.version.error')),
  })
}

/**
 * Shows another version of a question and the branch under it. The server
 * answers with the newest page of the branch now shown, which replaces the
 * newest rows the client holds (`spliceNewest`): the turns shared with the
 * branch just left stay, and the older pages the reader opened with them.
 */
export function useSwitchVersion(conversationId: string) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()

  return useMutation({
    // Shown in the transcript itself (the pager).
    meta: { silent: true },
    mutationFn: async ({ messageId }: { messageId: string }) =>
      unwrap(
        await api.POST('/conversations/{conversationId}/messages/{messageId}/switch', {
          params: { path: { conversationId, messageId }, query: { limit: PAGE_SIZE } },
        }),
      ),
    onSuccess: (page) => {
      // A finished run still held live would put its own rows back.
      const held = useChatStore.getState().live[conversationId]?.run?.run.id
      if (held) useChatStore.getState().settle(conversationId, held)
      queryClient.setQueryData<TranscriptPages>(queryKeys.messages.list(conversationId), (current) =>
        spliceNewest(current, page),
      )
    },
    onError: () => toast.error(t('chat.question-version.error')),
  })
}

/** Stops a run. What it generated so far is kept. */
export function useCancelRun(conversationId: string) {
  return useMutation({
    // Shown in the transcript itself (the outbox, a waiting reply, the pager).
    meta: { silent: true },
    mutationFn: async (runId: string) =>
      unwrap(
        await api.POST('/conversations/{conversationId}/runs/{runId}/cancel', {
          params: { path: { conversationId, runId } },
        }),
      ),
  })
}
