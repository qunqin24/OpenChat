import type { Document } from '@openchat/api-client'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useEffect } from 'react'

import { queryKeys } from '@openchat/chat-core/query-keys'

import { api, unwrap } from '@/lib/api'

// Writing blocks, as the web's hooks/use-documents.ts.

/**
 * A document and its versions. A card that names a version newer than the one
 * held (`minVersion`) - the model changed it since it was fetched - has it
 * fetched again.
 */
export function useDocument(id: string | null, minVersion = 0) {
  const queryClient = useQueryClient()
  const query = useQuery({
    queryKey: queryKeys.document(id ?? ''),
    queryFn: async ({ signal }) => unwrap(await api.GET('/documents/{documentId}', { params: { path: { documentId: id! } }, signal })),
    enabled: Boolean(id),
  })
  const latest = query.data?.versions.at(-1)?.version ?? 0
  const stale = Boolean(id && query.data && latest < minVersion && !query.isFetching)
  useEffect(() => {
    if (stale && id) void queryClient.invalidateQueries({ queryKey: queryKeys.document(id) })
  }, [stale, id, queryClient])
  return query
}

/** Saves the person's version on top of `baseVersion`; 409 `version_conflict` if the model wrote one since. */
export function useSaveDocument(id: string) {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async (body: { title: string; content: string; baseVersion: number }) =>
      unwrap(await api.PATCH('/documents/{documentId}', { params: { path: { documentId: id } }, body })),
    onSuccess: (doc: Document) => {
      queryClient.setQueryData(queryKeys.document(id), doc)
      void queryClient.invalidateQueries({ queryKey: queryKeys.conversationDocuments(doc.conversationId) })
    },
  })
}

/** The conversation's documents, the latest changed first. */
export function useConversationDocuments(conversationId: string | undefined) {
  return useQuery({
    queryKey: queryKeys.conversationDocuments(conversationId ?? ''),
    queryFn: async ({ signal }) =>
      unwrap(await api.GET('/conversations/{conversationId}/documents', { params: { path: { conversationId: conversationId! } }, signal })).items,
    enabled: Boolean(conversationId),
  })
}
