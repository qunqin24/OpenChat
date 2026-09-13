import { createFileRoute } from '@tanstack/react-router'

import { SettingsProfile } from '@/features/admin/settings/profile'

export const Route = createFileRoute('/admin/settings/')({
  component: SettingsProfile,
})
