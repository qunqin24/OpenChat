import { queryOptions, useQuery } from '@tanstack/react-query'

import { api, unwrap } from '@/lib/api'
import { queryKeys } from '@/lib/query-keys'

/**
 * Whether guests are let in: the administrator's switch. Public, so the sign-in
 * page can read it without a session, and it is the one guest call that is
 * answered whether or not the switch is on.
 */
export const guestStatusQueryOptions = queryOptions({
  queryKey: queryKeys.guest.status,
  queryFn: async ({ signal }) => unwrap(await api.GET('/guest/status', { signal })),
  // A switch an administrator flips rarely, read again when the page is.
  staleTime: 30_000,
  retry: false,
})

export function useGuestStatus() {
  return useQuery(guestStatusQueryOptions)
}
