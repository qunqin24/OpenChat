import { isOpenChatError, streamConversationEvents } from '@openchat/api-client'
import { useQueryClient } from '@tanstack/react-query'
import { useNavigate } from '@tanstack/react-router'
import { useEffect, useEffectEvent } from 'react'

import { refreshNewestMessages } from '@/hooks/use-messages'
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
 * hands over to Query: the newest page of the message list is refetched (the
 * older pages the reader has opened are immutable and stay), and only then is
 * the live copy dropped, so the answer never disappears in between.
 *
 * Another tab switching to a version of a question, or editing one, is
 * announced as `branch_changed`; the newest page is fetched again then too, so
 * the transcript here follows the branch the conversation now shows.
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

    // Brings the message list up to date: its newest page only, spliced onto
    // the older ones already held. If that fails, the plain refetch of every
    // page is the fallback.
    const refreshMessages = () =>
      refreshNewestMessages(queryClient, conversationId).catch(() =>
        queryClient.invalidateQueries({ queryKey: queryKeys.messages.list(conversationId) }),
      )

    // Hands a finished run over to the query cache.
    const handOver = (runId: string) => {
      void refreshMessages().then(() => settle(conversationId, runId))
      // The conversation's position in the sidebar and its model may have moved.
      void queryClient.invalidateQueries({ queryKey: queryKeys.conversations.all })
      // The model may have kept a note about the person while it ran.
      void queryClient.invalidateQueries({ queryKey: queryKeys.memory.all })
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
                void refreshMessages()
              }
              continue
            }
            applyBatch(conversationId, event)
            for (const runEvent of event.events) {
              if (runEvent.type === 'run_finished') {
                handOver(runEvent.run.id)
                // The composer's meter reads the request that run just made.
                void queryClient.invalidateQueries({ queryKey: queryKeys.conversations.context(conversationId) })
              }
              if (runEvent.type === 'run_started') {
                // Started elsewhere, or by us: either way the sidebar order moved.
                void queryClient.invalidateQueries({ queryKey: queryKeys.conversations.list() })
              }
              // Titled after its first answer. The batch carries the finished
              // run's last version, so the run state above has already seen
              // past it; the title is applied here, outside it.
              if (runEvent.type === 'title_changed') {
                void queryClient.invalidateQueries({ queryKey: queryKeys.conversations.all })
              }
              // The branch shown moved — a version was switched to or a
              // question edited, here or in another tab. The newest page is
              // what changed, so that is what is fetched (and spliced onto
              // the older pages that still hold): never the whole key, which
              // would download every page the reader had opened again.
              if (runEvent.type === 'branch_changed') {
                void refreshMessages().then(() => {
                  // A finished run still held live would put its own rows back
                  // over the branch now shown (settle leaves a running one be).
                  const held = useChatStore.getState().live[conversationId]?.run?.run.id
                  if (held) settle(conversationId, held)
                })
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
