import { createFileRoute } from '@tanstack/react-router'

import { FileStorage } from '@/features/admin/storage'

export const Route = createFileRoute('/admin/storage')({
  component: FileStorage,
})
