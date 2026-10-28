import type { QueryClient } from '@tanstack/react-query'

import { queryKeys } from '@/lib/query-keys'

/**
 * Deletions told to the other tabs of this browser.
 *
 * A page showing a conversation hears of its deletion on that conversation's
 * events stream (`conversation_deleted`) and leaves it. The other tabs follow
 * no stream for it, and their sidebars and searches would list it until their
 * next refetch, so the page that deleted tells them here and they drop it at
 * once. Another device finds out when it next reads the list.
 */
const CHANNEL = 'openchat-conversations'

type Message = { deleted: string[] | 'all' }

let channel: BroadcastChannel | undefined
const open = () => (channel ??= typeof BroadcastChannel === 'undefined' ? undefined : new BroadcastChannel(CHANNEL))

/** Tells the other tabs that these conversations (or all of them) were deleted. */
export function announceDeleted(deleted: string[] | 'all') {
  try {
    open()?.postMessage({ deleted } satisfies Message)
  } catch {
    // Telling the other tabs is a nicety; the deletion itself is done.
  }
}

/** Drops what this page holds about deleted conversations: their rows, and the lists that name them. */
export function forgetDeleted(queryClient: QueryClient, deleted: string[] | 'all') {
  if (deleted === 'all') {
    queryClient.removeQueries({ queryKey: queryKeys.messages.all })
    queryClient.removeQueries({ queryKey: [...queryKeys.conversations.all, 'detail'] })
    queryClient.removeQueries({ queryKey: [...queryKeys.conversations.all, 'context'] })
  } else {
    for (const id of deleted) {
      queryClient.removeQueries({ queryKey: queryKeys.conversations.detail(id) })
      queryClient.removeQueries({ queryKey: queryKeys.messages.list(id) })
    }
  }
  void queryClient.invalidateQueries({ queryKey: queryKeys.conversations.list() })
  void queryClient.invalidateQueries({ queryKey: [...queryKeys.conversations.all, 'search'] })
  void queryClient.invalidateQueries({ queryKey: queryKeys.conversations.running() })
}

/** Listens for the other tabs' deletions for as long as the page lives. */
export function followDeletions(queryClient: QueryClient) {
  const listening = open()
  if (!listening) return
  listening.onmessage = (event: MessageEvent<Partial<Message>>) => {
    const deleted = event.data?.deleted
    if (deleted === 'all' || (Array.isArray(deleted) && deleted.every((id) => typeof id === 'string'))) {
      forgetDeleted(queryClient, deleted)
    }
  }
}
