import { createFileRoute } from '@tanstack/react-router'

import { GenUIPreview } from '@/features/dev/GenUIPreview'

/** Unguarded on purpose: it renders a fixed sample, not data. */
export const Route = createFileRoute('/dev/genui')({
  component: GenUIPreview,
})
