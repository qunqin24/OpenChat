import type { MessagePage } from '@openchat/api-client'

import type { ConversationData, StoredMessage } from './types'

/**
 * A guest's conversation, read the way the server's queries read an account's
 * (AGENTS.md, "对话是一条线"): the transcript is every row of the conversation
 * in the order it was written, because what is replaced is deleted. These are
 * pure, and the storage module reads and writes.
 */

/**
 * A sort key for a time as the server writes it (RFC 3339 with up to
 * nanoseconds): UTC seconds, then the fraction at full width. Comparing two
 * keys as strings orders the instants - a Date would lose the microseconds that
 * tell two rows of one commit apart.
 */
export function timeKey(iso: string): string {
  const ms = Date.parse(iso)
  if (Number.isNaN(ms)) return '0000-00-00T00:00:00.000000000'
  const fraction = /\.(\d+)/.exec(iso)?.[1] ?? ''
  return `${new Date(ms).toISOString().slice(0, 19)}.${fraction.padEnd(9, '0').slice(0, 9)}`
}

/**
 * The time of a row the page writes itself (a refused message, the failed reply
 * of a run whose stream broke). Every other row has the server's time, and the
 * page's clock is not the server's - five minutes fast, a row stamped with it
 * sorts after the questions asked later - so a row of the page's own is put
 * where it belongs by the rows themselves: `server` if the server told the time
 * (and it is after the newest row there is), otherwise just after the newest
 * row, and with none at all, before everything the server will write. Never the
 * page's wall clock.
 */
export function stampAfter(newest: string | undefined, server?: string): string {
  const newestMs = newest ? Date.parse(newest) : Number.NaN
  if (server && !Number.isNaN(Date.parse(server)) && (Number.isNaN(newestMs) || timeKey(server) > timeKey(newest!))) return server
  return new Date(Number.isNaN(newestMs) ? 0 : newestMs + 1).toISOString()
}

/** `(created_at, id)` as one comparable string. */
export function rowKey(row: { createdAt: string; id: string }): string {
  return `${timeKey(row.createdAt)}|${row.id}`
}

export const byRow = (a: { createdAt: string; id: string }, b: { createdAt: string; id: string }) => {
  const x = rowKey(a)
  const y = rowKey(b)
  return x < y ? -1 : x > y ? 1 : 0
}

/** Every row the reader sees, oldest first. The refused messages are among them (see `contextOf`). */
export function transcriptOf(data: ConversationData): StoredMessage[] {
  return [...data.messages].sort(byRow)
}

/**
 * A message content moderation refused (status `blocked`): kept so that it is
 * still there when the page is opened again, shown, and never sent to a model.
 * It is the server's `messages.status = 'blocked'` row, and every rule below
 * treats it as the server's queries do (internal/db/queries/messages.sql).
 */
export const isBlocked = (row: { status: string }): boolean => row.status === 'blocked'

/**
 * The rows a model can be given or a summary can cover, in the order they were
 * given: the transcript less the refused messages. The server's context
 * queries (`ListMessagesAfter`, `ListMessagesFrom`, `ListRecentMessages`) leave
 * them out the same way.
 */
export function contextOf<T extends { status: string }>(rows: T[]): T[] {
  return rows.filter((row) => !isBlocked(row))
}

/**
 * The index of the last question that was asked: the last user row that was not
 * refused. A regeneration answers it (the server's `GetLastInput`); -1 when
 * there is none.
 */
export function lastQuestionIndex(rows: Pick<StoredMessage, 'role' | 'status'>[]): number {
  for (let i = rows.length - 1; i >= 0; i--) {
    const row = rows[i]!
    if (row.role === 'user' && !isBlocked(row)) return i
  }
  return -1
}

/**
 * The rows a redo deletes, as the server's DeleteTranscriptFrom and
 * DeleteAnswerTo choose them: with `from`, that message and every row after
 * it (an edited question); with `after`, the rows after it that the question
 * did not write itself (the answer of a regenerated one - a question is only
 * regenerated while it is the last, so what follows it is its answer - and the
 * messages refused since, which are user rows but not questions).
 */
export function deletedBy(
  messages: StoredMessage[],
  redo: { from: string } | { after: string },
): StoredMessage[] {
  const ordered = [...messages].sort(byRow)
  const id = 'from' in redo ? redo.from : redo.after
  const at = ordered.findIndex((m) => m.id === id)
  if (at < 0) return []
  return 'from' in redo ? ordered.slice(at) : ordered.slice(at + 1).filter((m) => m.role !== 'user' || isBlocked(m))
}

// ----------------------------------------------------------------- paging

/** A cursor is opaque to the page; this one is the key of the row it stands before. */
export const encodeCursor = (row: { createdAt: string; id: string }) => rowKey(row)

/**
 * A page of the transcript from its end (ListMessages): the newest rows, or
 * with a cursor the rows older than it, read oldest first; `olderCursor` is the
 * page's first row's when anything older remains.
 */
export function pageOf<T extends { createdAt: string; id: string }>(
  rows: T[],
  before: string | undefined,
  limit: number,
): { items: T[]; olderCursor: string | null } {
  const ordered = [...rows].sort(byRow)
  const older = before ? ordered.filter((r) => rowKey(r) < before) : ordered
  const items = older.slice(-limit)
  const first = items[0]
  return { items, olderCursor: older.length > items.length && first ? encodeCursor(first) : null }
}

export type { MessagePage }
