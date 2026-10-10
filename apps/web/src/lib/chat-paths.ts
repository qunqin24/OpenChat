import { useIsGuest } from '@/lib/guest/mode'

/**
 * Where the chat's links point. The same components draw an account's chat and
 * a guest's; the guest's pages live under /guest (routes/guest.tsx), so a link
 * to "a new chat" or "this conversation" is one address for one and another for
 * the other.
 */
export function useChatPaths() {
  const guest = useIsGuest()
  return {
    guest,
    home: guest ? ('/guest' as const) : ('/' as const),
    chat: guest ? ('/guest/chat/$conversationId' as const) : ('/chat/$conversationId' as const),
  }
}
