import { queryOptions, useQuery } from '@tanstack/react-query'

import { api, unwrap } from '@/lib/api'
import { queryKeys } from '@openchat/chat-core/query-keys'

/**
 * What the sign-in pages offer, as the administrator has set it: whether to
 * link to registration (and whether it will wait for approval). Public, so it
 * can be read without a session.
 */
export const authOptionsQueryOptions = queryOptions({
  queryKey: queryKeys.auth.options,
  queryFn: async ({ signal }) => unwrap(await api.GET('/auth/options', { signal })),
  // Switches an administrator flips rarely, read again when the page is.
  staleTime: 30_000,
  retry: false,
})

/**
 * Until the answer is in - or if it never comes - registration is treated as
 * open: the link is harmless, the server decides, and a page that hid it on a
 * failed request would lock a real new user out of the way in.
 */
export function useAuthOptions() {
  const query = useQuery(authOptionsQueryOptions)
  return {
    registration: query.data?.registration ?? true,
    passkeys: query.data?.passkeys ?? false,
    isPending: query.isPending,
  }
}
