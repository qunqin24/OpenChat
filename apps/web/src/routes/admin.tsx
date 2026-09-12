import { createFileRoute, redirect } from '@tanstack/react-router'

import AdminLayout from '@/components/admin/AdminLayout'
import { sessionQueryOptions } from '@/features/auth/use-auth'

/**
 * The admin panel sits outside `_authed` because it has its own shell — the
 * chat sidebar would make no sense here — so it repeats the session guard and
 * adds a role check on top.
 */
export const Route = createFileRoute('/admin')({
  beforeLoad: async ({ context, location }) => {
    const session = await context.queryClient.ensureQueryData(sessionQueryOptions)
    if (!session) {
      throw redirect({ to: '/login', search: { redirect: location.href } })
    }
    // Sent back to the app rather than shown a "forbidden" page: the server
    // answers 404 for the same reason, and a refusal confirms the panel exists.
    if (session.role !== 'admin') {
      throw redirect({ to: '/' })
    }
  },
  component: AdminLayout,
})
