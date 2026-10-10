import { createFileRoute, redirect } from '@tanstack/react-router'

import NextShell from '@/components/next/NextShell'
import { sessionQueryOptions } from '@/features/auth/use-auth'
import { chatFeaturesQueryOptions, chatModelsQueryOptions } from '@/hooks/use-chat-models'
import { conversationsQueryOptions } from '@/hooks/use-conversations'
import { adoptQueryCache } from '@/lib/query-persist'

/**
 * Pathless layout wrapping everything that requires a session. The guard runs
 * before the route loads, so a signed-out visitor never sees a flash of the
 * app shell.
 */
export const Route = createFileRoute('/_authed')({
  beforeLoad: async ({ context, location }) => {
    const { queryClient } = context
    // What every signed-in page shows, asked for alongside the session check
    // rather than after it and after the page's code: on a slow link each of
    // those waits is a whole round trip. Signed out, these fail and are
    // discarded with the redirect. Cached and fresh, they cost nothing.
    void queryClient.prefetchInfiniteQuery(conversationsQueryOptions)
    void queryClient.prefetchQuery(chatModelsQueryOptions)
    void queryClient.prefetchQuery(chatFeaturesQueryOptions)

    // `ensureQueryData` reuses the cached session when it is still fresh, so
    // navigating between protected routes costs no extra request.
    const session = await queryClient.ensureQueryData(sessionQueryOptions)
    // Before anything is drawn: a stored cache that is not this account's goes.
    adoptQueryCache(queryClient, session?.id ?? null)

    if (!session) {
      throw redirect({ to: '/login', search: { redirect: location.href } })
    }
  },
  component: NextShell,
})
