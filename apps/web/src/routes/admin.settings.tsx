import { createFileRoute } from '@tanstack/react-router'

import { ComingSoon } from '@/components/shadcn/coming-soon'

/**
 * Placeholder so the vendored navigation and profile menu have somewhere real
 * to point. Upstream splits settings into account / appearance / notifications
 * pages; none of them have anything to configure here yet.
 */
export const Route = createFileRoute('/admin/settings')({
  component: ComingSoon,
})
