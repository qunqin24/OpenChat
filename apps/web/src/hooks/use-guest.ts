import { queryOptions } from '@tanstack/react-query'

import { api, unwrap } from '@/lib/api'
import { queryKeys } from '@/lib/query-keys'

/**
 * Whether guests are let in: the administrator's switch. Public, and the one
 * guest call that is answered whether or not the switch is on. The /guest
 * routes check it before they open; the sign-in pages read the same switch
 * from `/auth/options` (hooks/use-auth-options.ts) along with what else they
 * offer.
 */
export const guestStatusQueryOptions = queryOptions({
  queryKey: queryKeys.guest.status,
  queryFn: async ({ signal }) => unwrap(await api.GET('/guest/status', { signal })),
  // A switch an administrator flips rarely, read again when the page is.
  staleTime: 30_000,
  retry: false,
})
