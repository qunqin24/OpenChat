import { createFileRoute } from '@tanstack/react-router'

import NextChat from '@/components/next/NextChat'

export const Route = createFileRoute('/chat/$conversationId')({
  component: ChatRoute,
})

function ChatRoute() {
  const { conversationId } = Route.useParams()
  return <NextChat key={conversationId} conversationId={conversationId} />
}
