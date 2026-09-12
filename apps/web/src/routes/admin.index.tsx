import { createFileRoute } from '@tanstack/react-router'

import DashboardPage from '@/components/admin/DashboardPage'

export const Route = createFileRoute('/admin/')({
  component: DashboardPage,
})
