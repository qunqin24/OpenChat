import { createFileRoute } from '@tanstack/react-router'

import { SettingsAccount } from '@/features/admin/settings/account'

export const Route = createFileRoute('/admin/settings/account')({
  component: SettingsAccount,
})
