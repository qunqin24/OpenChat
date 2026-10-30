import { streamConversationEvents } from '@openchat/api-client'
import { useQueryClient } from '@tanstack/react-query'
import { useNavigate } from '@tanstack/react-router'
import { useEffect, useEffectEvent } from 'react'

import { fetchNewestPage } from '@/hooks/use-messages'
import { API_BASE_URL } from '@/lib/api'
import { reportError } from '@/lib/query'
import { followConversation } from '@openchat/chat-core/conversation-follow'
import { useChatStore } from '@/stores/chat-store'

/**
 * Follows a conversation's runs for as long as the screen is open: the loop
 * and its rules are `chat-core/conversation-follow.ts`, shared with the mobile
 * app; this connects it with the browser's cookie and, when the conversation
 * is gone, takes the page to a new chat.
 */
export function useConversationEvents(conversationId: string | undefined) {
  const queryClient = useQueryClient()
  const navigate = useNavigate()

  // Read at the time it runs, so a new `navigate` does not restart the stream.
  const leave = useEffectEvent(() => void navigate({ to: '/', replace: true }))

  useEffect(() => {
    if (!conversationId) return
    const controller = new AbortController()
    void followConversation({
      conversationId,
      controller,
      events: (signal) =>
        streamConversationEvents({ baseUrl: API_BASE_URL, conversationId, credentials: 'include', signal }),
      queryClient,
      store: useChatStore,
      fetchNewest: () => fetchNewestPage(conversationId),
      leave,
      unauthorized: reportError,
      wait: (ms) => new Promise((resolve) => window.setTimeout(resolve, ms)),
    })
    return () => controller.abort()
  }, [conversationId, queryClient])
}
