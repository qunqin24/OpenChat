import { createFileRoute } from '@tanstack/react-router'

import { Dashboard } from '@/features/admin/dashboard'

export const Route = createFileRoute('/admin/')({
  component: Dashboard,
})
