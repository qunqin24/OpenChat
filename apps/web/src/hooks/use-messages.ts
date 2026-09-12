import { useQuery } from '@tanstack/react-query'

import { api, unwrap } from '@/lib/api'
import { queryKeys } from '@/lib/query-keys'

export function useMessages(conversationId: string) {
  return useQuery({
    queryKey: queryKeys.messages.list(conversationId),
    queryFn: async ({ signal }) =>
      unwrap(
        await api.GET('/conversations/{conversationId}/messages', {
          params: { path: { conversationId }, query: { limit: 100 } },
          signal,
        }),
      ),
    enabled: Boolean(conversationId),
  })
}
