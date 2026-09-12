import { createFileRoute } from '@tanstack/react-router'

import NextChat from '@/components/next/NextChat'

export const Route = createFileRoute('/')({
  component: HomeRoute,
})

/** Empty state: the composer is live here, and sending creates the conversation. */
function HomeRoute() {
  return <NextChat />
}
