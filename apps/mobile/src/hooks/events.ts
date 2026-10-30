import { streamConversationEvents } from '@openchat/api-client'
import { useQueryClient } from '@tanstack/react-query'
import { fetch as expoFetch } from 'expo/fetch'
import { useEffect, useRef, useState } from 'react'
import { AppState } from 'react-native'

import { followConversation } from '@openchat/chat-core/conversation-follow'

import { serverUrl } from '@/config'
import i18n from '@/lib/i18n'
import { reportError } from '@/lib/query'
import { useChatStore } from '@/lib/store'
import { currentToken } from '@/lib/token'
import { fetchNewestPage } from './messages'

/** Whether the app is on screen; a phone puts it away far more often than a browser hides a tab. */
function useForeground() {
  const [active, setActive] = useState(AppState.currentState === 'active')
  useEffect(() => {
    const subscription = AppState.addEventListener('change', (state) => setActive(state === 'active'))
    return () => subscription.remove()
  }, [])
  return active
}

/**
 * Follows a conversation's runs while it is on screen (chat-core
 * `followConversation`, the same loop as the web's). Put away, the app lets
 * the connection go - Android would cut it anyway - and on coming back
 * reconnects, which starts from a fresh snapshot: nothing is lost, the run
 * kept going on the server.
 *
 * Connects with Expo's fetch: React Native's own cannot read a body as it
 * arrives. `identity` keeps a proxy from compressing - and so buffering - it.
 */
export function useConversationEvents(conversationId: string | undefined, leave: () => void) {
  const queryClient = useQueryClient()
  const active = useForeground()
  const onLeave = useRef(leave)
  useEffect(() => {
    onLeave.current = leave
  })

  useEffect(() => {
    if (!conversationId || !active) return
    const controller = new AbortController()
    void followConversation({
      conversationId,
      controller,
      events: (signal) =>
        streamConversationEvents({
          baseUrl: `${serverUrl}/api/v1`,
          conversationId,
          signal,
          fetch: expoFetch as unknown as typeof globalThis.fetch,
          headers: {
            Authorization: `Bearer ${currentToken() ?? ''}`,
            'Accept-Language': i18n.language,
            'Accept-Encoding': 'identity',
          },
        }),
      queryClient,
      store: useChatStore,
      fetchNewest: () => fetchNewestPage(conversationId),
      leave: () => onLeave.current(),
      unauthorized: reportError,
      wait: (ms) => new Promise((resolve) => setTimeout(resolve, ms)),
    })
    return () => controller.abort()
  }, [conversationId, active, queryClient])
}

