import type { CompactionState, ContextSummary } from '@openchat/api-client'
import { useQuery } from '@tanstack/react-query'

import { queryKeys } from '@openchat/chat-core/query-keys'

import { api, unwrap } from '@/lib/api'

/** The conversation's compressed context, kept current by the events stream. */
export function useCompaction(conversationId: string | undefined): ContextSummary | undefined {
  const query = useQuery({
    queryKey: queryKeys.conversations.compaction(conversationId ?? ''),
    queryFn: async ({ signal }): Promise<CompactionState> =>
      unwrap(await api.GET('/conversations/{conversationId}/compaction', { params: { path: { conversationId: conversationId! } }, signal })),
    enabled: Boolean(conversationId),
  })
  return query.data?.summary
}
