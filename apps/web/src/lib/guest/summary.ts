import type { ContextSummary } from '@openchat/api-client'

import type { StoredMessage } from './types'

/**
 * A guest's compressed context, as the server keeps an account's
 * (`context_summaries`, internal/agent/summary.go). The rules are pure so that
 * they can be tested, and are the server's, so that both sides mean the same by
 * them: change one, change the other.
 *
 * A summary stands in, for the model, for the rows up to and including
 * `throughMessageId`, except the user message `keptMessageId`, which is sent as
 * written. The rows themselves are never touched.
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
  const ids = new Set(rows.map((m) => m.id))
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
export function rowsToSend(rows: StoredMessage[], summary: ContextSummary | undefined, max: number): StoredMessage[] {
  if (!summary) return rows.slice(-max)
  const through = rows.findIndex((m) => m.id === summary.throughMessageId)
  if (through < 0) return rows.slice(-max)
  const head: StoredMessage[] = []
  const kept = summary.keptMessageId ? rows.find((m) => m.id === summary.keptMessageId) : undefined
  if (kept && kept.id !== rows[through]!.id) head.push(kept)
  head.push(rows[through]!)
  return [...head, ...rows.slice(through + 1).slice(-Math.max(max - head.length, 0))]
}

/** Whether a row is an answer a model is sent (BuildContext leaves out the failed and the stopped). */
export function sentToModel(row: StoredMessage): boolean {
  if (row.role !== 'assistant') return false
  if (row.stopReason === 'error' || row.stopReason === 'cancelled') return false
  return row.parts.length > 0 || row.content.trim() !== ''
}

/** What a compression on request covers. */
export interface CompactionPlan {
  /** The last row the new summary covers. */
  through: StoredMessage
  /** The user message sent as written, if one is. */
  keep?: StoredMessage
  /** The rows to summarise, in order: the question the old summary kept (it was never written into it), then the rows since. */
  covered: StoredMessage[]
}

/**
 * What compressing the whole conversation covers (`ModeManual`): everything
 * since the old summary, the last question included. Except a question with no
 * answer to go with it - one that failed for the conversation's length, say -
 * which is kept as written and ends the summary, so that answering it again,
 * which deletes what follows it, leaves the summary in place. Null when there
 * is nothing since the last summary.
 */
export function planCompaction(rows: StoredMessage[], held: ContextSummary | undefined): CompactionPlan | null {
  const summary = survivingSummary(held, rows)
  const through = summary ? rows.findIndex((m) => m.id === summary.throughMessageId) : -1
  let since = rows.slice(through + 1)
  if (since.length === 0) return null
  const carried = summary?.keptMessageId ? rows.find((m) => m.id === summary.keptMessageId) : undefined

  let keep: StoredMessage | undefined
  let last = since.at(-1)!
  const lastUser = since.map((m) => m.role).lastIndexOf('user')
  // Only failures since a summary that kept the question: nothing to add.
  if (lastUser < 0 && carried && !since.some(sentToModel)) return null
  if (lastUser >= 0 && !since.slice(lastUser + 1).some(sentToModel)) {
    keep = since[lastUser]
    last = keep!
    since = since.slice(0, lastUser + 1)
  }
  const covered = [...(carried && carried.id !== keep?.id ? [carried] : []), ...since].filter((m) => m.id !== keep?.id)
  return { through: last, ...(keep ? { keep } : {}), covered }
}
