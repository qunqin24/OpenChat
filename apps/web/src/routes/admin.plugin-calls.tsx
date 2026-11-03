import { createFileRoute } from '@tanstack/react-router'

import { PluginCalls } from '@/features/admin/plugin-calls'

export const Route = createFileRoute('/admin/plugin-calls')({
  component: PluginCalls,
})
