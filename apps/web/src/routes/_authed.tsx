import { createFileRoute, redirect } from '@tanstack/react-router'

import ChatShell from '@/components/chat/ChatShell'
import { currentSession } from '@/features/auth/use-auth'
import { chatFeaturesQueryOptions, chatModelsQueryOptions } from '@/hooks/use-chat-models'
import { conversationsQueryOptions } from '@/hooks/use-conversations'
import { adoptQueryCache } from '@/lib/query-persist'
import { isSettingsTab, type SettingsTab } from '@/lib/settings'

/**
 * Pathless layout wrapping everything that requires a session. The guard runs
 * before the route loads, so a signed-out visitor never sees a flash of the
 * app shell.
 */
export const Route = createFileRoute('/_authed')({
  // The settings dialog is a search parameter, so every signed-in page can
  // open it and a link can name its tab.
  validateSearch: (search: Record<string, unknown>): { settings?: SettingsTab } =>
    isSettingsTab(search.settings) ? { settings: search.settings } : {},
  beforeLoad: async ({ context, location }) => {
    const { queryClient } = context
    // What every signed-in page shows, asked for alongside the session check
    // rather than after it and after the page's code: on a slow link each of
    // those waits is a whole round trip. Signed out, these fail and are
    // discarded with the redirect. Cached and fresh, they cost nothing.
    void queryClient.prefetchInfiniteQuery(conversationsQueryOptions)
    void queryClient.prefetchQuery(chatModelsQueryOptions)
    void queryClient.prefetchQuery(chatFeaturesQueryOptions)

    // A session checked within its stale time is reused, so navigating between
    // protected routes costs no request; an older one is asked of the server
    // again, which is how a disabled account or a changed password is noticed.
    const session = await currentSession(queryClient)
    // Before anything is drawn: a stored cache that is not this account's goes.
    adoptQueryCache(queryClient, session?.id ?? null)

    if (!session) {
      throw redirect({ to: '/login', search: { redirect: location.href } })
    }
  },
  component: ChatShell,
})
