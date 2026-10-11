import { createFileRoute } from '@tanstack/react-router'

import NextChat from '@/components/next/NextChat'
import { conversationQueryOptions } from '@/hooks/use-conversations'
import { messagesQueryOptions } from '@/hooks/use-messages'

export const Route = createFileRoute('/guest/chat/$conversationId')({
  // As the account's: not awaited, started while the page's code downloads.
  loader: ({ context, params }) => {
    void context.queryClient.prefetchQuery(conversationQueryOptions(params.conversationId))
    void context.queryClient.prefetchInfiniteQuery(messagesQueryOptions(params.conversationId))
  },
  component: GuestChatRoute,
})

function GuestChatRoute() {
  const { conversationId } = Route.useParams()
  return <NextChat key={conversationId} conversationId={conversationId} />
}
