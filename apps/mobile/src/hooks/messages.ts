import { useInfiniteQuery, type QueryClient } from '@tanstack/react-query'

import { refreshNewestMessages as refreshNewest } from '@openchat/chat-core/conversation-follow'
import { queryKeys } from '@openchat/chat-core/query-keys'
import { transcriptItems } from '@openchat/chat-core/transcript-pages'

import { api, unwrap } from '@/lib/api'

/** Rows per request, the most the server hands out. */
const PAGE_SIZE = 100

const fetchPage = async (conversationId: string, before: string | undefined, signal?: AbortSignal) =>
  unwrap(
    await api.GET('/conversations/{conversationId}/messages', {
      params: { path: { conversationId }, query: { limit: PAGE_SIZE, before } },
      signal,
    }),
  )

/** The newest page of a transcript, fetched afresh. */
export const fetchNewestPage = (conversationId: string) => fetchPage(conversationId, undefined)

/**
 * A conversation's transcript, from its end, a page at a time (as the web's
 * hooks/use-messages.ts): `fetchNextPage` loads older rows.
 */
export function useMessages(conversationId: string | undefined) {
  return useInfiniteQuery({
    queryKey: queryKeys.messages.list(conversationId ?? ''),
    initialPageParam: undefined as string | undefined,
    queryFn: ({ signal, pageParam }) => fetchPage(conversationId!, pageParam, signal),
    getNextPageParam: (page) => page.olderCursor ?? undefined,
    enabled: Boolean(conversationId),
    select: transcriptItems,
  })
}

/** The newest rows brought up to date, older pages kept (chat-core `refreshNewestMessages`). */
export function refreshNewestMessages(queryClient: QueryClient, conversationId: string) {
  return refreshNewest(queryClient, conversationId, () => fetchNewestPage(conversationId))
}
