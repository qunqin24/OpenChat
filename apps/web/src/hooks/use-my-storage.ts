import { useQuery } from '@tanstack/react-query'

import { api, unwrap } from '@/lib/api'
import { queryKeys } from '@/lib/query-keys'

/** What the person's files take against their quota (settings → 数据). */
export function useMyStorage() {
  return useQuery({
    queryKey: queryKeys.storage,
    queryFn: async ({ signal }) => unwrap(await api.GET('/me/storage', { signal })),
  })
}
