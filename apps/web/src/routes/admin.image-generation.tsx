import { createFileRoute } from '@tanstack/react-router'

import { ImageGenerationPage } from '@/features/admin/image-generation'

export const Route = createFileRoute('/admin/image-generation')({
  component: ImageGenerationPage,
})
