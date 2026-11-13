import { createFileRoute, redirect } from '@tanstack/react-router'

import { SYSTEM_TABS, type SystemTab } from '@/features/admin/system/tabs'

/**
 * Where the system settings were, under the personal settings, until they got
 * a page of their own: a bookmark or a link from before still lands on the
 * same tab there.
 */
export const Route = createFileRoute('/admin/settings/system')({
  validateSearch: (search: Record<string, unknown>): { tab?: SystemTab } => ({
    tab: SYSTEM_TABS.find((tab) => tab === search.tab),
  }),
  beforeLoad: ({ search }) => {
    throw redirect({ to: '/admin/system', search, replace: true })
  },
})
