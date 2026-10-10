import { dehydrate, hydrate, type DehydratedState, type Query, type QueryClient } from '@tanstack/react-query'

import { resetChatStore } from '@/stores/chat-store'

import { queryKeys } from './query-keys'

/**
 * Keeps what the chat screen opens with — the conversation list, the models,
 * the features and the last few conversations read — in localStorage, so a
 * page reload draws them at once and refreshes them behind the scenes, instead
 * of showing spinners for as many round trips as it takes. On a slow link that
 * is the difference between seconds and nothing.
 *
 * Scoped to one account: the cache records whose it is, and is dropped the
 * moment a different account (or none) is signed in, and on sign-out. It is
 * the reader's own data in the reader's own browser, but on a shared computer
 * the next person must not see it. The session itself is never stored: whether
 * you are signed in is always asked of the server.
 *
 * Everything here is best effort. Storage may be full, blocked or wiped; then
 * the page simply loads from the network as it would without this.
 */

const STORAGE_KEY = 'openchat.cache.v1'
/** How many conversations' transcripts to keep, most recently fetched first. */
const RECENT_CONVERSATIONS = 5
/** Past this the transcripts are left out, then everything. */
const MAX_BYTES = 1_500_000
const SAVE_DELAY = 1000

interface Stored {
  owner: string
  state: DehydratedState
}

let owner: string | null = null
let restoredOwner: string | null = null

const isKey = (query: Query, prefix: readonly unknown[]) => prefix.every((part, i) => query.queryKey[i] === part)

const always = [queryKeys.conversations.list(), queryKeys.models.list(), queryKeys.features]

/** Put back what the last visit stored. Before the first render. */
export function restoreQueryCache(client: QueryClient) {
  try {
    const raw = localStorage.getItem(STORAGE_KEY)
    if (!raw) return
    const stored = JSON.parse(raw) as Stored
    // Drawn at once, never trusted: dated as old as can be, so every query
    // refetches when first used however recently it was saved. Kept at their
    // real age, anything saved within the default stale time would be taken
    // as fresh and never checked — a reload right after a change would show
    // the page as it was before it.
    for (const query of stored.state.queries) query.state.dataUpdatedAt = 0
    hydrate(client, stored.state)
    restoredOwner = stored.owner
  } catch {
    // Unreadable or blocked: start from the network.
  }
}

/**
 * Called once the session is known, before anything is drawn. A cache that
 * belongs to someone else is thrown away, then saving starts for this user.
 */
export function adoptQueryCache(client: QueryClient, userId: string | null) {
  const previous = owner ?? restoredOwner
  if (previous !== null && previous !== userId) {
    // Everything, not only what is stored: the memory, personalization and
    // admin queries of the last account are as much theirs. Only the session
    // (which the caller has just asked for, and which says who this is) stays.
    client.removeQueries({ predicate: (query) => query.queryKey[0] !== queryKeys.auth.session[0] })
    resetChatStore()
    forget()
  }
  restoredOwner = userId
  owner = userId
}

/** Sign-out: nothing of this account stays behind. */
export function forgetQueryCache() {
  owner = null
  restoredOwner = null
  forget()
}

function forget() {
  try {
    localStorage.removeItem(STORAGE_KEY)
  } catch {
    // Nothing to do.
  }
}

/** Saves the chosen queries a moment after they change. */
export function persistQueryCache(client: QueryClient) {
  let timer: ReturnType<typeof setTimeout> | undefined
  client.getQueryCache().subscribe((event) => {
    if (event.type !== 'updated' && event.type !== 'removed') return
    clearTimeout(timer)
    timer = setTimeout(() => save(client), SAVE_DELAY)
  })
  // A reload or a closed tab inside the delay would lose the last change.
  window.addEventListener('pagehide', () => {
    clearTimeout(timer)
    save(client)
  })
}

function save(client: QueryClient) {
  const who = owner
  if (!who) return
  // A temporary chat is not kept anywhere, this browser's storage included.
  const temporary = new Set(
    client
      .getQueryCache()
      .findAll({ queryKey: [...queryKeys.conversations.all, 'detail'] })
      .filter((q) => (q.state.data as { temporary?: boolean } | undefined)?.temporary)
      .map((q) => String(q.queryKey[2])),
  )
  // The transcripts most recently fetched, and those conversations' details.
  const recent = new Set(
    client
      .getQueryCache()
      .findAll({ queryKey: queryKeys.messages.all })
      .filter((q) => q.state.status === 'success' && !temporary.has(String(q.queryKey[2])))
      .sort((a, b) => b.state.dataUpdatedAt - a.state.dataUpdatedAt)
      .slice(0, RECENT_CONVERSATIONS)
      .map((q) => String(q.queryKey[2])),
  )

  const write = (withTranscripts: boolean) => {
    const state = dehydrate(client, {
      shouldDehydrateQuery: (query) => {
        if (query.state.status !== 'success') return false
        if (always.some((prefix) => isKey(query, prefix))) return true
        if (!withTranscripts) return false
        const id = String(query.queryKey[2])
        return (
          (isKey(query, queryKeys.messages.all) || isKey(query, [...queryKeys.conversations.all, 'detail'])) &&
          recent.has(id)
        )
      },
    })
    // Only the first page of an infinite query: the conversation list's newest
    // and a transcript's newest rows. The rest load on scroll, and a transcript's
    // older pages are never stored — they could fill the quota on their own.
    for (const query of state.queries) {
      const data = query.state.data as { pages?: unknown[]; pageParams?: unknown[] } | undefined
      if (data?.pages && data.pages.length > 1) {
        query.state.data = { pages: data.pages.slice(0, 1), pageParams: data.pageParams?.slice(0, 1) }
      }
    }
    return JSON.stringify({ owner: who, state } satisfies Stored)
  }

  try {
    let text = write(true)
    if (text.length > MAX_BYTES) text = write(false)
    if (text.length > MAX_BYTES) return forget()
    localStorage.setItem(STORAGE_KEY, text)
  } catch {
    // Full or blocked: the next visit loads from the network.
  }
}
