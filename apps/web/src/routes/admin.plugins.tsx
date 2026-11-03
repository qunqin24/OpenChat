import { createFileRoute } from '@tanstack/react-router'

import { AdminPlugins } from '@/features/admin/plugins'

export const Route = createFileRoute('/admin/plugins')({
  component: AdminPlugins,
})
