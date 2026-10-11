import type { InfiniteData } from '@tanstack/react-query'
import type { Message, MessagePage } from '@openchat/api-client'

/**
 * A conversation's transcript as the query cache holds it, from its end: the
 * first page is the newest rows and each further page the rows just older than
 * the last one (the server's `olderCursor`). A page's own items read oldest
 * first.
 *
 * These are the pure moves on it; the hooks in `hooks/use-messages.ts` and
 * `hooks/use-send-message.ts` apply them to the cache.
 */
export type TranscriptPages = InfiniteData<MessagePage, string | undefined>

/**
 * The transcript of a conversation that has just been made: no rows, nothing
 * older. Put in the cache for it, so the page that made it does not fetch what
 * it knows is empty - and show a spinner in place of the message just sent.
 */
export function emptyTranscript(): TranscriptPages {
  return { pages: [{ items: [], olderCursor: null }], pageParams: [undefined] }
}

/**
 * `held` with its newest rows replaced by `fresh`, the page just fetched
 * without a cursor.
 *
 * Rows can also have been deleted since `held` was fetched (an edit removes a
 * question and everything after it, a regeneration an answer). That is always
 * a suffix of the transcript, so what `fresh` does not repeat, of what is held,
 * is either older than it and still there, or gone: if `fresh` starts at a row
 * `held` has, the rows before that one are kept and everything after is
 * replaced; if it starts at a row `held` does not have, nothing held is known
 * to lie before it, and `fresh` replaces it all.
 */
export function spliceNewest(held: TranscriptPages | undefined, fresh: MessagePage): TranscriptPages {
  const only: TranscriptPages = { pages: [fresh], pageParams: [undefined] }
  if (!held || held.pages.length === 0) return only

  const rows = held.pages.slice().reverse().flatMap((page) => page.items)
  // Where the fresh page begins among the rows held. Not among them: it starts
  // before everything held, or reaches past it - or what it starts at is new,
  // the rest of the transcript having been deleted from under it - and it
  // replaces it all. Older rows load again as the reader asks for them.
  const firstId = fresh.items[0]?.id
  const at = firstId ? rows.findIndex((m) => m.id === firstId) : -1
  if (at <= 0) return only

  return {
    pages: [fresh, { items: rows.slice(0, at), olderCursor: held.pages.at(-1)!.olderCursor }],
    pageParams: [undefined, fresh.olderCursor ?? undefined],
  }
}

/**
 * `held` without the rows a redo has deleted: with `from`, that message and
 * every row after it (an edit); with `after`, every row after that message (a
 * regeneration). The server has done it already; this is the cache catching
 * up before the next fetch. A message that is not among the rows held leaves
 * them as they are - the cache has caught up already (the events stream got
 * there first), or the message is older than anything loaded.
 */
export function truncateTranscript(
  held: TranscriptPages | undefined,
  target: { from?: string; after?: string },
): TranscriptPages | undefined {
  if (!held || held.pages.length === 0) return held
  const rows = held.pages.slice().reverse().flatMap((page) => page.items)
  const at = rows.findIndex((m) => m.id === (target.from ?? target.after))
  if (at < 0) return held
  const kept = rows.slice(0, target.from ? at : at + 1)
  return {
    pages: [{ items: kept, olderCursor: held.pages.at(-1)!.olderCursor }],
    pageParams: [undefined],
  }
}

/**
 * `held` with a row added at the end of the transcript: a message the server
 * has just kept (a refused send, which no run follows) shown before the page has
 * fetched it again. Nothing is made of an empty cache - the fetch that fills it
 * will have the row - and a row already held is not added twice.
 */
export function appendRow(held: TranscriptPages | undefined, row: Message): TranscriptPages | undefined {
  if (!held || held.pages.length === 0) return held
  if (held.pages.some((page) => page.items.some((m) => m.id === row.id))) return held
  const [newest, ...older] = held.pages
  return { ...held, pages: [{ ...newest!, items: [...newest!.items, row] }, ...older] }
}
