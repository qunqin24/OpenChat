import type { Message, MessagePage } from '@openchat/api-client'
import {
  infiniteQueryOptions,
  useInfiniteQuery,
  type InfiniteData,
  type QueryClient,
} from '@tanstack/react-query'

import { api, unwrap } from '@/lib/api'
import { queryKeys } from '@/lib/query-keys'

/** Rows per request, the most the server hands out. */
export const PAGE_SIZE = 100

type Cursor = string | undefined
export type TranscriptPages = InfiniteData<MessagePage, Cursor>

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

/** Every loaded row in reading order. The pages overlap only after a splice, hence the id check. */
function flatten(data: TranscriptPages): { items: Message[] } {
  const seen = new Set<string>()
  const items: Message[] = []
  for (const page of [...data.pages].reverse()) {
    for (const message of page.items) {
      if (seen.has(message.id)) continue
      seen.add(message.id)
      items.push(message)
    }
  }
  return { items }
}

/**
 * The loaded part of a transcript, oldest first. `hasNextPage` means older rows
 * remain on the server and `fetchNextPage` loads the next older page.
 */
export function useMessages(conversationId: string) {
  return useInfiniteQuery({
    ...messagesQueryOptions(conversationId),
    enabled: Boolean(conversationId),
    select: flatten,
  })
}

/**
 * Brings the newest rows up to date — what a finished run wrote — without
 * downloading the older pages again. They are immutable, so the fresh newest
 * page is spliced onto what is already held: rows older than it stay, and the
 * reader's revealed history (and the scroll position it holds up) does not
 * move.
 */
export async function refreshNewestMessages(queryClient: QueryClient, conversationId: string) {
  const fresh = await fetchPage(conversationId, undefined)
  queryClient.setQueryData<TranscriptPages>(queryKeys.messages.list(conversationId), (held) =>
    spliceNewest(held, fresh),
  )
}

/** `held` with its newest rows replaced by `fresh`, the page just fetched without a cursor. */
export function spliceNewest(held: TranscriptPages | undefined, fresh: MessagePage): TranscriptPages {
  const only: TranscriptPages = { pages: [fresh], pageParams: [undefined] }
  if (!held || held.pages.length === 0) return only

  const rows = held.pages.slice().reverse().flatMap((page) => page.items)
  // Where the fresh page begins among the rows held. Not among them: it starts
  // before everything held, or reaches past it — either way it replaces it,
  // and older rows load again as the reader asks for them.
  const firstId = fresh.items[0]?.id
  const at = firstId ? rows.findIndex((m) => m.id === firstId) : -1
  if (at <= 0) return only

  return {
    pages: [fresh, { items: rows.slice(0, at), olderCursor: held.pages.at(-1)!.olderCursor }],
    pageParams: [undefined, fresh.olderCursor ?? undefined],
  }
}
