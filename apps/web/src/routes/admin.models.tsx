import { createFileRoute } from '@tanstack/react-router'

import { Models } from '@/features/admin/models'

export const Route = createFileRoute('/admin/models')({
  component: Models,
})
