import { createFileRoute } from '@tanstack/react-router'

import { conversationQueryOptions } from '@/hooks/use-conversations'
import { messagesQueryOptions } from '@/hooks/use-messages'

/** A conversation: an address only, drawn by the chat layout (`_authed._chat.tsx`). */
export const Route = createFileRoute('/_authed/_chat/chat/$conversationId')({
  // Not awaited: the page renders at once and its queries pick up these
  // requests already in flight, started while its code is still downloading.
  // The router also runs this when a link to the conversation is hovered
  // (`defaultPreload: 'intent'`), so a conversation picked from the sidebar
  // usually opens from cache.
  loader: ({ context, params }) => {
    void context.queryClient.prefetchQuery(conversationQueryOptions(params.conversationId))
    void context.queryClient.prefetchInfiniteQuery(messagesQueryOptions(params.conversationId))
  },
})
