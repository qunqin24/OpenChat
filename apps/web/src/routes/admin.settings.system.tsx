import { createFileRoute } from '@tanstack/react-router'

import { SettingsSystem } from '@/features/admin/settings/system'
import { SYSTEM_TABS, type SystemTab } from '@/features/admin/settings/system/tabs'

export const Route = createFileRoute('/admin/settings/system')({
  validateSearch: (search: Record<string, unknown>): { tab?: SystemTab } => ({
    tab: SYSTEM_TABS.find((tab) => tab === search.tab),
  }),
  component: SettingsSystem,
})
