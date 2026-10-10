import { createFileRoute } from '@tanstack/react-router'

import { UsageLogs } from '@/features/admin/usage-logs'

/**
 * What the dashboard's links can ask the list to start with: the failures, or
 * one model. Anything else in the address is dropped rather than trusted.
 */
export const Route = createFileRoute('/admin/usage-logs')({
  validateSearch: (search: Record<string, unknown>): { status?: 'error'; model?: string } => ({
    ...(search.status === 'error' ? { status: 'error' as const } : {}),
    ...(typeof search.model === 'string' && search.model ? { model: search.model } : {}),
  }),
  component: UsageLogsPage,
})

function UsageLogsPage() {
  return <UsageLogs initialFilters={Route.useSearch()} />
}
