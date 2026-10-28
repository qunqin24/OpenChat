import type { Document } from '@openchat/api-client'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useEffect } from 'react'

import { api, unwrap } from '@/lib/api'
import { queryKeys } from '@/lib/query-keys'

/** A writing block with every version, oldest first. */
export function useDocument(id: string | null, minVersion = 0) {
  const queryClient = useQueryClient()
  const query = useQuery({
    queryKey: queryKeys.document(id ?? ''),
    queryFn: async ({ signal }) =>
      unwrap(await api.GET('/documents/{documentId}', { params: { path: { documentId: id! } }, signal })),
    enabled: Boolean(id),
  })
  // A reply that has just written a version the cache has not seen: ask again.
  const latest = query.data?.versions.at(-1)?.version ?? 0
  const stale = Boolean(id && query.data && latest < minVersion && !query.isFetching)
  useEffect(() => {
    if (stale && id) void queryClient.invalidateQueries({ queryKey: queryKeys.document(id) })
  }, [stale, id, queryClient])
  return query
}

/** Saves the person's edit as a new version. */
export function useSaveDocument(id: string) {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async (body: { title: string; content: string; baseVersion: number }) =>
      unwrap(await api.PATCH('/documents/{documentId}', { params: { path: { documentId: id } }, body })),
    onSuccess: (doc: Document) => {
      queryClient.setQueryData(queryKeys.document(id), doc)
      // The list is ordered by the last change, and shows the title.
      void queryClient.invalidateQueries({ queryKey: queryKeys.conversationDocuments(doc.conversationId) })
    },
  })
}

/** A conversation's writing blocks, the most recently changed first. */
export function useConversationDocuments(conversationId: string | undefined, enabled = true) {
  return useQuery({
    queryKey: queryKeys.conversationDocuments(conversationId ?? ''),
    queryFn: async ({ signal }) =>
      unwrap(
        await api.GET('/conversations/{conversationId}/documents', { params: { path: { conversationId: conversationId! } }, signal }),
      ).items,
    enabled: Boolean(conversationId) && enabled,
  })
}
