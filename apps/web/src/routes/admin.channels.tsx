import { createFileRoute } from '@tanstack/react-router'

import { Channels } from '@/features/admin/channels'

export const Route = createFileRoute('/admin/channels')({
  component: Channels,
})
