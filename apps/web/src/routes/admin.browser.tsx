import { createFileRoute } from '@tanstack/react-router'

import { BrowserSettingsPage } from '@/features/admin/browser'

export const Route = createFileRoute('/admin/browser')({
  component: BrowserSettingsPage,
})
