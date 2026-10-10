import { isOpenChatError, type ReasoningEffort } from '@openchat/api-client'
import { useMutation } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { api, unwrap } from '@/lib/api'
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
] as const

/**
 * Sends a message. The server writes it, starts the run that answers it and
 * returns at once; the answer arrives on the conversation's events stream, so
 * nothing here waits for it — and closing the page does not stop it.
 */
export function useSendMessage(conversationId: string) {
  const { t } = useTranslation()
  const startRun = useChatStore((s) => s.startRun)

  return useMutation({
    mutationFn: async ({
      content,
      model,
      reasoningEffort,
      webSearch,
      attachmentIds,
    }: {
      content: string
      model?: string
      reasoningEffort?: ReasoningEffort
      webSearch?: boolean
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
            ...(model ? { model } : {}),
            ...(reasoningEffort ? { reasoningEffort } : {}),
            ...(webSearch !== undefined ? { webSearch } : {}),
            ...(attachmentIds?.length ? { attachmentIds } : {}),
          },
        }),
      ),
    onSuccess: (response) => startRun(conversationId, response.run, response.message),
    onError: (error) => {
      const code = isOpenChatError(error) ? error.code : ''
      const known = SEND_ERRORS.find((c) => c === code)
      toast.error(known ? t(`chat.send-error.${known}`) : t('chat.send-error.generic'))
    },
  })
}

/** Stops a run. What it generated so far is kept. */
export function useCancelRun(conversationId: string) {
  return useMutation({
    mutationFn: async (runId: string) =>
      unwrap(
        await api.POST('/conversations/{conversationId}/runs/{runId}/cancel', {
          params: { path: { conversationId, runId } },
        }),
      ),
  })
}
