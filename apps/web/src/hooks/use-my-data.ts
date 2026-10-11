import { useMutation, useQueryClient } from '@tanstack/react-query'

import { api, API_BASE_URL, appFetch, unwrap } from '@/lib/api'
import { announceDeleted, forgetDeleted } from '@/lib/conversation-sync'

/**
 * The person's own data as a whole: take it out, or delete it. Both are
 * `/me` calls, so they can only ever reach the signed-in account's.
 */

/** The file name the server offers, else a dated one of our own. */
function exportName(response: Response): string {
  const offered = /filename="([^"]+)"/.exec(response.headers.get('Content-Disposition') ?? '')?.[1]
  return offered ?? `openchat-export-${new Date().toISOString().slice(0, 10)}.json`
}

/**
 * Downloads every kept conversation as one JSON file. The server writes it as
 * it reads; here it is gathered into a file and handed to the browser, which
 * saves it. A reply that stops short (the server cuts the connection when it
 * fails midway) rejects the body, so a broken file is never offered.
 */
export function useExportConversations() {
  return useMutation({
    // The button says it is working.
    meta: { silent: true },
    mutationFn: async () => {
      const response = await appFetch(`${API_BASE_URL}/me/export`, { credentials: 'include' })
      if (!response.ok) throw new Error(`export failed: ${response.status}`)
      const name = exportName(response)
      const url = URL.createObjectURL(await response.blob())
      try {
        const link = document.createElement('a')
        link.href = url
        link.download = name
        document.body.append(link)
        link.click()
        link.remove()
      } finally {
        // After the click has been handled; revoking at once can cancel it.
        window.setTimeout(() => URL.revokeObjectURL(url), 10_000)
      }
    },
  })
}

/** Deletes every conversation, then drops everything the page held about them. */
export function useDeleteAllConversations() {
  const queryClient = useQueryClient()

  return useMutation({
    meta: { silent: true },
    mutationFn: async () => unwrap(await api.DELETE('/me/conversations')),
    onSuccess: () => {
      forgetDeleted(queryClient, 'all')
      announceDeleted('all')
    },
  })
}
