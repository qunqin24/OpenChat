import { createFileRoute } from '@tanstack/react-router'

import { conversationQueryOptions } from '@/hooks/use-conversations'
import { messagesQueryOptions } from '@/hooks/use-messages'

/** A guest's conversation: an address only, drawn by the chat layout (`guest._chat.tsx`). */
export const Route = createFileRoute('/guest/_chat/chat/$conversationId')({
  // As the account's: not awaited, started while the page's code downloads.
  loader: ({ context, params }) => {
    void context.queryClient.prefetchQuery(conversationQueryOptions(params.conversationId))
    void context.queryClient.prefetchInfiniteQuery(messagesQueryOptions(params.conversationId))
  },
})
