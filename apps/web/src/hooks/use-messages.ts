import { queryOptions, useQuery } from '@tanstack/react-query'

import { api, unwrap } from '@/lib/api'
import { queryKeys } from '@/lib/query-keys'

/** A conversation's transcript. Shared with the route loader, which starts it early. */
export const messagesQueryOptions = (conversationId: string) =>
  queryOptions({
    queryKey: queryKeys.messages.list(conversationId),
    queryFn: async ({ signal }) =>
      unwrap(
        await api.GET('/conversations/{conversationId}/messages', {
          params: { path: { conversationId }, query: { limit: 100 } },
          signal,
        }),
      ),
  })

export function useMessages(conversationId: string) {
  return useQuery({ ...messagesQueryOptions(conversationId), enabled: Boolean(conversationId) })
}
