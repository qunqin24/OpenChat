import { createFileRoute } from '@tanstack/react-router'

import { Settings } from '@/features/admin/settings'

export const Route = createFileRoute('/admin/settings')({
  component: Settings,
})
