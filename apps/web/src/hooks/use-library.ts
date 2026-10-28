import type { LibraryFile } from '@openchat/api-client'
import { useInfiniteQuery, useMutation, useQueryClient } from '@tanstack/react-query'

import { api, unwrap } from '@/lib/api'
import { markRemoved, withoutFile, type LibraryFilter, type LibraryPages } from '@/lib/library'
import { queryKeys } from '@/lib/query-keys'
import type { TranscriptPages } from '@/lib/transcript-pages'

/**
 * The person's sent files, newest first, a page at a time. Fetched afresh on
 * every visit: the links in it are signed and expire, and files come and go
 * with the conversations they were sent in.
 */
export function useLibraryFiles(search: string, filter: LibraryFilter) {
  return useInfiniteQuery({
    queryKey: queryKeys.library.list(search, filter),
    initialPageParam: undefined as string | undefined,
    queryFn: async ({ signal, pageParam }) =>
      unwrap(
        await api.GET('/files', {
          params: {
            query: {
              limit: 60,
              cursor: pageParam,
              ...(search ? { search } : {}),
              ...(filter !== 'all' ? { type: filter } : {}),
            },
          },
          signal,
        }),
      ),
    getNextPageParam: (page) => page.nextCursor ?? undefined,
    placeholderData: (previous) => previous,
    staleTime: 0,
    gcTime: 5 * 60_000,
  })
}

/**
 * Saves a file: asks for a link whose response is an attachment, then follows
 * it. The page stays where it is; the browser saves.
 */
export async function downloadFile(fileId: string) {
  const { url } = unwrap(await api.GET('/files/{fileId}/download', { params: { path: { fileId } } }))
  const link = document.createElement('a')
  link.href = url
  link.rel = 'noopener'
  document.body.append(link)
  link.click()
  link.remove()
}

/**
 * Deletes a file for good. The library forgets it, and a transcript in the
 * cache that holds it shows it as deleted, as the server will from now on.
 */
export function useDeleteFile() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async (file: LibraryFile) =>
      unwrap(await api.DELETE('/files/{fileId}', { params: { path: { fileId: file.id } } })),
    onSuccess: (_data, file) => {
      queryClient.setQueriesData<LibraryPages>({ queryKey: queryKeys.library.all }, (held) => withoutFile(held, file.id))
      queryClient.setQueryData<TranscriptPages>(queryKeys.messages.list(file.conversationId), (held) => markRemoved(held, file.id))
    },
  })
}
