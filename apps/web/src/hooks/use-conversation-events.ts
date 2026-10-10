import { isOpenChatError, streamConversationEvents } from '@openchat/api-client'
import { useQueryClient } from '@tanstack/react-query'
import { useNavigate } from '@tanstack/react-router'
import { useEffect, useEffectEvent } from 'react'

import { API_BASE_URL } from '@/lib/api'
import { queryKeys } from '@/lib/query-keys'
import { useChatStore } from '@/stores/chat-store'

/** Reconnect delays, in ms: quick at first, then backing off to a ceiling. */
const RECONNECT_DELAYS = [500, 1000, 2000, 5000, 10000, 15000]

/**
 * Follows a conversation's runs for as long as the screen is open.
 *
 * Opening the page attaches to whatever is in flight — a run started in
 * another tab, or one that kept going while this page was closed — and a
 * dropped connection reattaches from a fresh snapshot. A run that finishes
 * hands over to Query: the message list is refetched, and only then is the
 * live copy dropped, so the answer never disappears in between.
 *
 * A conversation the server says does not exist — deleted from another tab or
 * device while this page was open — is not a dropped connection: there is
 * nothing to reattach to, so following stops and the page goes to a new chat.
 */
export function useConversationEvents(conversationId: string | undefined) {
  const queryClient = useQueryClient()
  const navigate = useNavigate()

  // Read at the time it runs, so a new `navigate` does not restart the stream.
  const leave = useEffectEvent(() => {
    void queryClient.invalidateQueries({ queryKey: queryKeys.conversations.all })
    void navigate({ to: '/', replace: true })
  })

  useEffect(() => {
    if (!conversationId) return
    const controller = new AbortController()
    const { applySnapshot, applyBatch, settle } = useChatStore.getState()

    // Hands a finished run over to the query cache.
    const handOver = (runId: string) => {
      void queryClient
        .invalidateQueries({ queryKey: queryKeys.messages.list(conversationId) })
        .then(() => settle(conversationId, runId))
      // The conversation's position in the sidebar and its model may have moved.
      void queryClient.invalidateQueries({ queryKey: queryKeys.conversations.all })
    }

    const follow = async () => {
      let attempt = 0
      while (!controller.signal.aborted) {
        try {
          for await (const event of streamConversationEvents({
            baseUrl: API_BASE_URL,
            conversationId,
            credentials: 'include',
            signal: controller.signal,
          })) {
            attempt = 0
            if (event.type === 'snapshot') {
              const before = useChatStore.getState().live[conversationId]?.run
              applySnapshot(conversationId, event)
              // A run that finished while we were away: its rows are only in
              // the message list now.
              if (before && before.run.id !== event.run?.id) {
                void queryClient.invalidateQueries({ queryKey: queryKeys.messages.list(conversationId) })
              }
              continue
            }
            applyBatch(conversationId, event)
            for (const runEvent of event.events) {
              if (runEvent.type === 'run_finished') handOver(runEvent.run.id)
              if (runEvent.type === 'run_started') {
                // Started elsewhere, or by us: either way the sidebar order moved.
                void queryClient.invalidateQueries({ queryKey: queryKeys.conversations.list() })
              }
            }
          }
        } catch (error) {
          if (controller.signal.aborted) return
          if (isOpenChatError(error) && error.status === 404) {
            leave()
            return
          }
        }
        if (controller.signal.aborted) return
        const delay = RECONNECT_DELAYS[Math.min(attempt, RECONNECT_DELAYS.length - 1)]
        attempt++
        await new Promise((resolve) => window.setTimeout(resolve, delay))
      }
    }

    void follow()
    return () => controller.abort()
  }, [conversationId, queryClient])
}
