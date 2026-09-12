import { createFileRoute, redirect } from '@tanstack/react-router'

import NextShell from '@/components/next/NextShell'
import { sessionQueryOptions } from '@/features/auth/use-auth'

/**
 * Pathless layout wrapping everything that requires a session. The guard runs
 * before the route loads, so a signed-out visitor never sees a flash of the
 * app shell.
 */
export const Route = createFileRoute('/_authed')({
  beforeLoad: async ({ context, location }) => {
    // `ensureQueryData` reuses the cached session when it is still fresh, so
    // navigating between protected routes costs no extra request.
    const session = await context.queryClient.ensureQueryData(sessionQueryOptions)

    if (!session) {
      throw redirect({ to: '/login', search: { redirect: location.href } })
    }
  },
  component: NextShell,
})
