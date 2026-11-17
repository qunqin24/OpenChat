import { queryOptions } from '@tanstack/react-query'
import { createContext } from 'react'

import { api, API_BASE_URL, unwrap } from '@/lib/api'
import { queryKeys } from '@openchat/chat-core/query-keys'

/**
 * The conversation an answer's links belong to, for their cards: the server
 * reads only pages that conversation's tool results list. Null where there is
 * none to ask about (a sub-agent's transcript), and the card shows the address.
 */
export const LinkPreviews = createContext<string | null>(null)

export function linkPreviewQuery(conversationId: string, url: string) {
  return queryOptions({
    queryKey: queryKeys.linkPreview(conversationId, url),
    queryFn: async ({ signal }) =>
      unwrap(await api.GET('/conversations/{conversationId}/link-preview', { params: { path: { conversationId }, query: { url } }, signal })),
    // A page's title and picture change rarely, and the server keeps them a day.
    staleTime: 60 * 60_000,
    // 404 is the answer for a link no source listed, not a failure to retry.
    retry: false,
  })
}

/** The page's sharing picture, through the server. */
export function linkPreviewImage(conversationId: string, url: string) {
  return `${API_BASE_URL}/conversations/${encodeURIComponent(conversationId)}/link-preview/image?url=${encodeURIComponent(url)}`
}
