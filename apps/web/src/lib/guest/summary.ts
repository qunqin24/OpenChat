import type { ContextSummary } from '@openchat/api-client'

import { contextOf } from './transcript'
import type { StoredMessage } from './types'

/**
 * A guest's compressed context, as the server keeps an account's
 * (`context_summaries`, internal/agent/summary.go). The rules are pure so that
 * they can be tested, and are the server's, so that both sides mean the same by
 * them: change one, change the other.
 *
 * A summary stands in, for the model, for the rows up to and including
 * `throughMessageId`, except the user message `keptMessageId`, which is sent as
 * written. The rows themselves are never touched. A message moderation refused
 * (status `blocked`) is not a row of any of this: it is never covered, ended at,
 * kept or sent.
 */

/**
 * The summary, if it still describes this transcript. Editing a question
 * deletes it and every row after it, and regenerating deletes the answer after
 * the last question: a summary whose last row or kept question went with them
 * went too. On the server that is a cascading foreign key; here it is this check,
 * made wherever rows are deleted and again wherever the summary is read.
 */
export function survivingSummary(summary: ContextSummary | null | undefined, rows: StoredMessage[]): ContextSummary | undefined {
  if (!summary) return undefined
  // A refused message is never what a summary ends at or keeps (the server's
  // queries leave it out), so one that names it describes nothing.
  const ids = new Set(contextOf(rows).map((m) => m.id))
  if (!ids.has(summary.throughMessageId)) return undefined
  if (summary.keptMessageId && !ids.has(summary.keptMessageId)) return undefined
  return summary
}

/**
 * The rows a run's request carries, given the transcript in order. Without a
 * summary, the newest `max`. With one: the question it kept, the row it ends at
 * (the server writes the summary over it, so it has to exist), and the rows
 * after that - the rows between, which the summary replaces, are not sent. At
 * most `max` rows in all.
 */
export function rowsToSend(all: StoredMessage[], summary: ContextSummary | undefined, max: number): StoredMessage[] {
  // Refused messages are never sent.
  const rows = contextOf(all)
  if (!summary) return rows.slice(-max)
  const through = rows.findIndex((m) => m.id === summary.throughMessageId)
  if (through < 0) return rows.slice(-max)
  const head: StoredMessage[] = []
  const kept = summary.keptMessageId ? rows.find((m) => m.id === summary.keptMessageId) : undefined
  if (kept && kept.id !== rows[through]!.id) head.push(kept)
  head.push(rows[through]!)
  return [...head, ...rows.slice(through + 1).slice(-Math.max(max - head.length, 0))]
}
