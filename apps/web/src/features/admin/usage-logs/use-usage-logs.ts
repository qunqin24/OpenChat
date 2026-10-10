import type { components } from '@openchat/api-client'
import { useQuery } from '@tanstack/react-query'

import { api, unwrap } from '@/lib/api'

type Schemas = components['schemas']

export type UsageLog = Schemas['UsageLog']
export type UsageSummary = Schemas['UsageSummary']
export type UsageStatus = Schemas['UsageStatus']
export type UsageKind = Schemas['UsageKind']

/** What the toolbar narrows the list by. An absent field is no constraint. */
export type UsageFilters = {
  userId?: string
  model?: string
  channelId?: string
  status?: UsageStatus
  kind?: UsageKind
  /** Local calendar days, `YYYY-MM-DD`; both ends inclusive. */
  from?: string
  to?: string
}

/**
 * A local calendar day as an instant. The server's range is half-open, so the
 * end of a day is the start of the next one, and the time zone is the
 * operator's: "today" means their today, not the server's.
 */
function dayStart(day: string, plusDays = 0): string | undefined {
  const [year, month, date] = day.split('-').map(Number)
  if (!year || !month || !date) return undefined
  return new Date(year, month - 1, date + plusDays).toISOString()
}

/**
 * Nested under `admin`, so the broad invalidation the admin mutations already
 * do reaches it; and carrying every filter, so each combination is its own
 * cache entry.
 */
export const usageLogKeys = {
  all: ['admin', 'usage-logs'] as const,
  page: (page: number, limit: number, filters: UsageFilters) =>
    [...usageLogKeys.all, page, limit, filters] as const,
}

export function useUsageLogs({
  page,
  limit,
  filters,
}: {
  page: number
  limit: number
  filters: UsageFilters
}) {
  return useQuery({
    queryKey: usageLogKeys.page(page, limit, filters),
    queryFn: async ({ signal }) =>
      unwrap(
        await api.GET('/admin/usage-logs', {
          params: {
            query: {
              page,
              limit,
              ...(filters.userId ? { userId: filters.userId } : {}),
              ...(filters.model ? { model: filters.model } : {}),
              ...(filters.channelId ? { channelId: filters.channelId } : {}),
              ...(filters.status ? { status: filters.status } : {}),
              ...(filters.kind ? { kind: filters.kind } : {}),
              ...(filters.from ? { from: dayStart(filters.from) } : {}),
              ...(filters.to ? { to: dayStart(filters.to, 1) } : {}),
            },
          },
          signal,
        }),
      ),
    // The filters are in the key: without this the table would empty out and
    // re-lay-out on every change, and the totals above it would blink.
    placeholderData: (previous) => previous,
  })
}
