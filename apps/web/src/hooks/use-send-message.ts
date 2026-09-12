import { streamMessage } from '@openchat/api-client'
import { useQueryClient } from '@tanstack/react-query'
import { useCallback } from 'react'

import { API_BASE_URL } from '@/lib/api'
import { queryKeys } from '@/lib/query-keys'
import { useChatStore } from '@/stores/chat-store'

/**
 * Sends a message and drains the SSE stream into the chat store. The persisted
 * message list is refetched once the stream closes, so Query stays the single
 * source of truth for anything durable.
 */
export function useSendMessage(conversationId: string) {
  const queryClient = useQueryClient()

  const start = useChatStore((s) => s.start)
  const setMessageId = useChatStore((s) => s.setMessageId)
  const appendDelta = useChatStore((s) => s.appendDelta)
  const fail = useChatStore((s) => s.fail)
  const finish = useChatStore((s) => s.finish)

  return useCallback(
    async (content: string, attachmentIds?: string[]) => {
      const trimmed = content.trim()
      if (!trimmed) return

      const signal = start(conversationId)

      try {
        for await (const event of streamMessage({
          baseUrl: API_BASE_URL,
          conversationId,
          body: { content: trimmed, ...(attachmentIds?.length ? { attachmentIds } : {}) },
          signal,
        })) {
          switch (event.type) {
            case 'start':
              setMessageId(event.messageId)
              // The user turn is already persisted, so pull it in right away.
              void queryClient.invalidateQueries({
                queryKey: queryKeys.messages.list(conversationId),
              })
              break
            case 'delta':
              appendDelta(event.text)
              break
            case 'done':
              finish()
              break
            case 'error':
              fail(event.error.message)
              break
          }
        }
      } catch (error) {
        // An abort is the user pressing Stop, not a failure.
        if (signal.aborted) finish()
        else fail(error instanceof Error ? error.message : 'stream failed')
      } finally {
        await queryClient.invalidateQueries({ queryKey: queryKeys.messages.list(conversationId) })
        void queryClient.invalidateQueries({ queryKey: queryKeys.conversations.list() })
        if (useChatStore.getState().stream?.error === null) finish()
      }
    },
    [appendDelta, conversationId, fail, finish, queryClient, setMessageId, start],
  )
}
