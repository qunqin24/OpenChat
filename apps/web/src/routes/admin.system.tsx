import { createFileRoute } from '@tanstack/react-router'

import { SystemSettings } from '@/features/admin/system'
import { SYSTEM_TABS, type SystemTab } from '@/features/admin/system/tabs'

export const Route = createFileRoute('/admin/system')({
  validateSearch: (search: Record<string, unknown>): { tab?: SystemTab } => ({
    tab: SYSTEM_TABS.find((tab) => tab === search.tab),
  }),
  component: SystemSettings,
})
