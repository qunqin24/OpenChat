import { createFileRoute, redirect } from '@tanstack/react-router'

import { AuthenticatedLayout } from '@/components/shadcn/layout/authenticated-layout'
import { currentSession } from '@/features/auth/use-auth'

/**
 * The admin panel sits outside `_authed` because it has its own shell — the
 * chat sidebar would make no sense here — so it repeats the session guard and
 * adds a role check on top.
 */
export const Route = createFileRoute('/admin')({
  beforeLoad: async ({ context }) => {
    const session = await currentSession(context.queryClient)
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
 * The admin panel's frame. Its page-load bar is the admin side's way of showing
 * work (the chat shows its own in place and has no bar), but it is not drawn
 * here: this layout is not mounted yet during the first navigation into the
 * panel, so the bar lives at the root (AdminProgress) and watches for admin routes.
 */
function AdminLayout() {
  return <AuthenticatedLayout />
}
