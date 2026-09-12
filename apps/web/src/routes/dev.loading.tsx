import { createFileRoute } from '@tanstack/react-router'

import { LoadingPreview } from '@/features/dev/LoadingPreview'

/** Unguarded on purpose: it shows animations, not data. */
export const Route = createFileRoute('/dev/loading')({
  component: LoadingPreview,
})
