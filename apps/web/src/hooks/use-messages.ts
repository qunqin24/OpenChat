import { infiniteQueryOptions, useInfiniteQuery, type QueryClient } from '@tanstack/react-query'

import { api, unwrap } from '@/lib/api'
import { queryKeys } from '@openchat/chat-core/query-keys'
import { refreshNewestMessages as refreshNewest } from '@openchat/chat-core/conversation-follow'
import { transcriptItems } from '@openchat/chat-core/transcript-pages'

/** Rows per request, the most the server hands out. */
export const PAGE_SIZE = 100

type Cursor = string | undefined

const fetchPage = async (conversationId: string, before: Cursor, signal?: AbortSignal) =>
  unwrap(
    await api.GET('/conversations/{conversationId}/messages', {
      params: { path: { conversationId }, query: { limit: PAGE_SIZE, before } },
      signal,
    }),
  )

/**
 * A conversation's transcript, from its end: the first page is the newest
 * rows and each further page the rows just older than the last one (the
 * server's `olderCursor`). Pages are kept newest first, as fetched; a page's
 * own items read oldest first. Shared with the route loader, which starts the
 * first page early.
 */
export const messagesQueryOptions = (conversationId: string) =>
  infiniteQueryOptions({
    queryKey: queryKeys.messages.list(conversationId),
    initialPageParam: undefined as Cursor,
    queryFn: ({ signal, pageParam }) => fetchPage(conversationId, pageParam, signal),
    getNextPageParam: (page) => page.olderCursor ?? undefined,
  })

/**
 * The loaded part of a transcript, oldest first. `hasNextPage` means older rows
 * remain on the server and `fetchNextPage` loads the next older page.
 */
export function useMessages(conversationId: string) {
  return useInfiniteQuery({
    ...messagesQueryOptions(conversationId),
    enabled: Boolean(conversationId),
    select: transcriptItems,
  })
}

/** The newest rows brought up to date, older pages kept (chat-core `refreshNewestMessages`). */
export function refreshNewestMessages(queryClient: QueryClient, conversationId: string) {
  return refreshNewest(queryClient, conversationId, () => fetchNewestPage(conversationId))
}

/** The newest page of a transcript, fetched afresh. */
export const fetchNewestPage = (conversationId: string) => fetchPage(conversationId, undefined)
