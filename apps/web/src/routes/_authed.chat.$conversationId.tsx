import { createFileRoute } from '@tanstack/react-router'

import NextChat from '@/components/next/NextChat'
import { conversationQueryOptions } from '@/hooks/use-conversations'
import { messagesQueryOptions } from '@/hooks/use-messages'

export const Route = createFileRoute('/_authed/chat/$conversationId')({
  // Not awaited: the page renders at once and its queries pick up these
  // requests already in flight, started while its code is still downloading.
  // The router also runs this when a link to the conversation is hovered
  // (`defaultPreload: 'intent'`), so a conversation picked from the sidebar
  // usually opens from cache.
  loader: ({ context, params }) => {
    void context.queryClient.prefetchQuery(conversationQueryOptions(params.conversationId))
    void context.queryClient.prefetchInfiniteQuery(messagesQueryOptions(params.conversationId))
  },
  component: ChatRoute,
})

function ChatRoute() {
  const { conversationId } = Route.useParams()
  return <NextChat key={conversationId} conversationId={conversationId} />
}
