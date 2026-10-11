import { createFileRoute } from '@tanstack/react-router'

import { ModerationLogs } from '@/features/admin/moderation-logs'

export const Route = createFileRoute('/admin/moderation-logs')({
  component: ModerationLogs,
})
