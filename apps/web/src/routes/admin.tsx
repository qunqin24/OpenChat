import { createFileRoute, redirect } from '@tanstack/react-router'

import { GlobalProgress } from '@/components/GlobalProgress'
import { AuthenticatedLayout } from '@/components/shadcn/layout/authenticated-layout'
import { sessionQueryOptions } from '@/features/auth/use-auth'

/**
 * The admin panel sits outside `_authed` because it has its own shell — the
 * chat sidebar would make no sense here — so it repeats the session guard and
 * adds a role check on top.
 */
export const Route = createFileRoute('/admin')({
  beforeLoad: async ({ context }) => {
    const session = await context.queryClient.ensureQueryData(sessionQueryOptions)
    if (!session) {
      // Signing in lands in chat; the admin panel is opened explicitly there.
      throw redirect({ to: '/login' })
    }
    // Sent back to the app rather than shown a "forbidden" page: the server
    // answers 404 for the same reason, and a refusal confirms the panel exists.
    if (session.role !== 'admin') {
      throw redirect({ to: '/' })
    }
  },
  component: AdminLayout,
})

/**
 * The admin panel's frame, with its page-load bar. The bar is the admin
 * side's way of showing work; the chat shows its own in place (a waiting
 * reply, a spinner where the transcript will be) and has no bar.
 */
function AdminLayout() {
  return (
    <>
      <GlobalProgress />
      <AuthenticatedLayout />
    </>
  )
}
