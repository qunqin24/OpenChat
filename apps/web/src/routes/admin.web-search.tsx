import { createFileRoute } from '@tanstack/react-router'

import { WebSearch } from '@/features/admin/web-search'

export const Route = createFileRoute('/admin/web-search')({
  component: WebSearch,
})
