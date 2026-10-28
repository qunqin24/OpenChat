import { createFileRoute } from '@tanstack/react-router'

import { ImageSearch } from '@/features/admin/image-search'

export const Route = createFileRoute('/admin/image-search')({
  component: ImageSearch,
})
