import { createFileRoute, redirect } from '@tanstack/react-router'

import NextShell from '@/components/next/NextShell'
import { guestStatusQueryOptions } from '@/hooks/use-guest'
import { chatFeaturesQueryOptions, chatModelsQueryOptions } from '@/hooks/use-chat-models'
import { conversationsQueryOptions } from '@/hooks/use-conversations'
import { isSettingsTab, type SettingsTab } from '@/lib/settings'

/**
 * The chat for someone with no account. Same shell as `_authed`, but the
 * conversations are the browser's own (lib/guest), so there is no session to
 * check - what is checked is that the administrator lets guests in. The
 * address is /guest, outside `_authed`, so nothing here can reach an account's.
 */
export const Route = createFileRoute('/guest')({
  validateSearch: (search: Record<string, unknown>): { settings?: SettingsTab } =>
    isSettingsTab(search.settings) ? { settings: search.settings } : {},
  beforeLoad: async ({ context }) => {
    const { queryClient } = context
    const status = await queryClient.ensureQueryData(guestStatusQueryOptions).catch(() => ({ enabled: false }))
    // Off: there is nothing here, and the sign-in page is where to go.
    if (!status.enabled) throw redirect({ to: '/login', search: {} })
    // Opens the browser's storage now rather than on the first message, so the
    // notice can say early if conversations will not be kept. Loaded here, not
    // imported, so an account never downloads the guest code.
    void import('@/lib/guest').then((guest) => guest.startGuest()).catch(() => undefined)
    void queryClient.prefetchInfiniteQuery(conversationsQueryOptions)
    void queryClient.prefetchQuery(chatModelsQueryOptions)
    void queryClient.prefetchQuery(chatFeaturesQueryOptions)
  },
  component: NextShell,
})
