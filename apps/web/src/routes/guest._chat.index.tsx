import { createFileRoute } from '@tanstack/react-router'

import NextChat from '@/components/next/NextChat'

export const Route = createFileRoute('/guest/')({
  component: GuestHomeRoute,
})

/** Empty state: the composer is live here, and sending creates the conversation. */
function GuestHomeRoute() {
  return <NextChat />
}
