import { isOpenChatError, type Attachment, type ConversationReference } from '@openchat/api-client'
import { useMutation, useQueryClient, type QueryClient } from '@tanstack/react-query'
import * as Crypto from 'expo-crypto'
import { useTranslation } from 'react-i18next'

import type { AskAnswer } from '@openchat/chat-core/ask-user'
import { CAPACITY_CODE } from '@openchat/chat-core/model-capacity'
import { refusalOf, type Refusal } from '@openchat/chat-core/moderation-refusal'
import { queryKeys } from '@openchat/chat-core/query-keys'
import { choiceBody, forgetDeleted, keepRefused as keepRefusedRow, sendErrorKey, type RunChoice } from '@openchat/chat-core/send'

import { api, unwrap } from '@/lib/api'
import { useChatStore } from '@/lib/store'
import { toast } from '@/lib/toast'
import { refreshNewestMessages } from './messages'

// Sending, regenerating, editing, answering and stopping, as the web's
// hooks/use-send-message.ts does them; the shared parts are chat-core/send.ts.

const requestId = () => Crypto.randomUUID()

function useSendError(conversationId?: string) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  return (error: Error) => {
    // A signed-out account is taken to sign-in; nothing to say here.
    if (isOpenChatError(error) && error.status === 401) return
    const refusal = refusalOf(error)
    if (refusal) {
      toast.error(
        t('chat.send-error.content_blocked'),
        refusal.strikesLeft ? t('chat.refused.warning', { count: refusal.strikesLeft, hours: refusal.windowHours }) : undefined,
      )
      return
    }
    const id = isOpenChatError(error) ? error.requestId : undefined
    toast.error(t(sendErrorKey(error)), id ? t('chat.error-id', { id }) : undefined)
    const code = isOpenChatError(error) ? error.code : ''
    // The picker learned of the cap too late: fetch it again so the model shows as unavailable.
    if (code === CAPACITY_CODE) void queryClient.invalidateQueries({ queryKey: queryKeys.models.all })
    // The conversation ends on a refused message this screen did not know of:
    // fetch its end, and the composer becomes that message's editor.
    if (code === 'revise_refused' && conversationId) void refreshNewestMessages(queryClient, conversationId).catch(() => undefined)
  }
}

function keepRefused(queryClient: QueryClient, conversationId: string, refusal: Refusal, content: string) {
  keepRefusedRow(queryClient, useChatStore, conversationId, refusal, content, () =>
    refreshNewestMessages(queryClient, conversationId),
  )
}

/**
 * Sends a message. The server writes it, starts the run that answers it and
 * returns at once; the answer arrives on the events stream. To the screen's
 * conversation, or to the one just made for a new chat's first message.
 */
export function useSendMessage(conversationId: string) {
  const startRun = useChatStore((s) => s.startRun)
  const unpost = useChatStore((s) => s.unpost)
  const queryClient = useQueryClient()
  const onError = useSendError(conversationId)

  return useMutation({
    meta: { silent: true },
    mutationFn: async ({
      conversationId: target = conversationId,
      content,
      attachmentIds,
      conversationIds,
      ...choice
    }: RunChoice & { conversationId?: string; content: string; attachmentIds?: string[]; conversationIds?: string[] }) =>
      unwrap(
        await api.POST('/conversations/{conversationId}/messages', {
          params: { path: { conversationId: target } },
          body: {
            content,
            requestId: requestId(),
            ...choiceBody(choice),
            ...(attachmentIds?.length ? { attachmentIds } : {}),
            ...(conversationIds?.length ? { conversationIds } : {}),
          },
        }),
      ),
    onSuccess: (response, variables) => startRun(variables.conversationId ?? conversationId, response.run, response.message),
    // Hook-level, not the call's own: that one is skipped once the screen that
    // made the call has gone, and the outbox bubble would stay for ever.
    onError: (error, variables) => {
      const target = variables.conversationId ?? conversationId
      const refusal = refusalOf(error)
      if (refusal?.messageId) keepRefused(queryClient, target, refusal, variables.content)
      unpost(target)
      onError(error)
    },
  })
}

/** Answers the last message again; the answer it replaces is deleted. */
export function useRegenerate(conversationId: string) {
  const queryClient = useQueryClient()
  const startRun = useChatStore((s) => s.startRun)
  const redo = useChatStore((s) => s.redo)
  const onError = useSendError(conversationId)

  return useMutation({
    meta: { silent: true },
    mutationFn: async ({ inputId: _, ...choice }: RunChoice & { inputId: string }) =>
      unwrap(
        await api.POST('/conversations/{conversationId}/regenerate', {
          params: { path: { conversationId } },
          body: { requestId: requestId(), ...choiceBody(choice) },
        }),
      ),
    onMutate: ({ inputId }) => redo(conversationId, { after: inputId }),
    onSuccess: (response) => {
      forgetDeleted(queryClient, conversationId, { after: response.message.id })
      startRun(conversationId, response.run, response.message)
    },
    onError: (error) => {
      redo(conversationId, null)
      onError(error)
    },
  })
}

/** Asks a question again in new words: it and everything after it are deleted, the new wording goes on the end. */
export function useEditMessage(conversationId: string) {
  const queryClient = useQueryClient()
  const startRun = useChatStore((s) => s.startRun)
  const redo = useChatStore((s) => s.redo)
  const post = useChatStore((s) => s.post)
  const unpost = useChatStore((s) => s.unpost)
  const onError = useSendError(conversationId)

  return useMutation({
    meta: { silent: true },
    mutationFn: async ({
      messageId,
      content,
      attachments: _attachments,
      references: _references,
      attachmentIds,
      conversationIds,
      ...choice
    }: RunChoice & {
      messageId: string
      content: string
      attachments: Attachment[]
      references?: ConversationReference[]
      attachmentIds?: string[]
      conversationIds?: string[]
    }) =>
      unwrap(
        await api.POST('/conversations/{conversationId}/messages/{messageId}/edit', {
          params: { path: { conversationId, messageId } },
          body: {
            content,
            requestId: requestId(),
            ...choiceBody(choice),
            ...(attachmentIds?.length ? { attachmentIds } : {}),
            ...(conversationIds?.length ? { conversationIds } : {}),
          },
        }),
      ),
    onMutate: ({ messageId, content, attachments, references }) => {
      redo(conversationId, { from: messageId })
      post(conversationId, { content, attachments, references })
    },
    onSuccess: (response, { messageId }) => {
      forgetDeleted(queryClient, conversationId, { from: messageId })
      startRun(conversationId, response.run, response.message)
    },
    onError: (error, variables) => {
      // Words of a refused message refused again took its place: show the new row.
      const refusal = refusalOf(error)
      if (refusal?.messageId) {
        forgetDeleted(queryClient, conversationId, { from: variables.messageId })
        keepRefused(queryClient, conversationId, refusal, variables.content)
      }
      redo(conversationId, null)
      unpost(conversationId)
      onError(error)
    },
  })
}

/** Answers the question the model asked; the run that goes on from it arrives on the stream. */
export function useAnswerQuestion(conversationId: string) {
  const startRun = useChatStore((s) => s.startRun)
  const onError = useSendError(conversationId)
  return useMutation({
    meta: { silent: true },
    mutationFn: async ({ callId, answers, ...choice }: RunChoice & { callId: string; answers: AskAnswer[] }) =>
      unwrap(
        await api.POST('/conversations/{conversationId}/tool-calls/{callId}/answer', {
          params: { path: { conversationId, callId } },
          body: { answers, requestId: requestId(), ...choiceBody(choice) },
        }),
      ),
    onSuccess: (response) => startRun(conversationId, response.run, response.message, [response.result]),
    onError: (error) => onError(error),
  })
}

/** Stops a run. What it generated so far is kept. */
export function useCancelRun(conversationId: string) {
  return useMutation({
    meta: { silent: true },
    mutationFn: async (runId: string) =>
      unwrap(await api.POST('/conversations/{conversationId}/runs/{runId}/cancel', { params: { path: { conversationId, runId } } })),
  })
}
