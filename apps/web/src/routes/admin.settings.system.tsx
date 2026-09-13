import { createFileRoute } from '@tanstack/react-router'

import { SettingsSystem } from '@/features/admin/settings/system'

export const Route = createFileRoute('/admin/settings/system')({
  component: SettingsSystem,
})
