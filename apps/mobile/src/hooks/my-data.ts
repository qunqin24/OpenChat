import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { cacheDirectory, deleteAsync, downloadAsync, moveAsync } from 'expo-file-system/legacy'
import * as Sharing from 'expo-sharing'

import { queryKeys } from '@openchat/chat-core/query-keys'

import { serverUrl } from '@/config'
import { api, unwrap } from '@/lib/api'
import { currentToken } from '@/lib/token'

/**
 * The person's own data as a whole (the web's use-my-data.ts): what their
 * files take, taking it all out, and deleting every conversation. All are
 * `/me` calls, so they can only ever reach the signed-in account's.
 */

/** What the person's files take against their quota. */
export function useMyStorage() {
  return useQuery({
    queryKey: queryKeys.storage,
    queryFn: async ({ signal }) => unwrap(await api.GET('/me/storage', { signal })),
  })
}

/** The file name the server offers, else a dated one of our own. */
function exportName(headers: Record<string, string>): string {
  const disposition = Object.entries(headers).find(([key]) => key.toLowerCase() === 'content-disposition')?.[1] ?? ''
  return /filename="([^"]+)"/.exec(disposition)?.[1] ?? `openchat-export-${new Date().toISOString().slice(0, 10)}.json`
}

/**
 * Every kept conversation as one JSON file, handed to the system's share
 * sheet - to save to Files or Drive, or send on. It is written to the cache
 * first: the server writes it as it reads, and a reply cut short is not
 * offered as if it were whole.
 */
export function useExportConversations() {
  return useMutation({
    meta: { silent: true },
    mutationFn: async () => {
      const temporary = `${cacheDirectory}openchat-export.json`
      const result = await downloadAsync(`${serverUrl}/api/v1/me/export`, temporary, {
        headers: { Authorization: `Bearer ${currentToken() ?? ''}` },
      })
      if (result.status !== 200) throw new Error(`export failed: ${result.status}`)
      // Renamed to the server's name, which is what the share sheet shows.
      const named = `${cacheDirectory}${exportName(result.headers).replace(/[^\w.-]+/g, '_')}`
      if (named !== temporary) {
        await deleteAsync(named, { idempotent: true })
        await moveAsync({ from: temporary, to: named })
      }
      await Sharing.shareAsync(named, { mimeType: 'application/json', UTI: 'public.json' })
    },
  })
}

/** Deletes every conversation, then drops everything the app held about them. */
export function useDeleteAllConversations() {
  const queryClient = useQueryClient()
  return useMutation({
    meta: { silent: true },
    mutationFn: async () => unwrap(await api.DELETE('/me/conversations')),
    onSuccess: () => {
      queryClient.removeQueries({ queryKey: queryKeys.messages.all })
      queryClient.removeQueries({ queryKey: [...queryKeys.conversations.all, 'detail'] })
      queryClient.removeQueries({ queryKey: [...queryKeys.conversations.all, 'context'] })
      void queryClient.invalidateQueries({ queryKey: queryKeys.conversations.all })
      // Their files went with them.
      void queryClient.invalidateQueries({ queryKey: queryKeys.storage })
    },
  })
}
