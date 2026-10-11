import type { ContextSummary, GuestReceipts, Memory } from '@openchat/api-client'

import type { StoredMessage, StoredPersonalization } from './types'

/**
 * Receipts: what the server vouches for in a guest's conversation.
 *
 * The conversation is kept here, in the browser, and the browser could write
 * anything into it - so the server believes none of it. It signs what it has
 * checked (the words a person wrote, once moderated) or written (the model's
 * rows, the summaries and notes it made), and a request is read for what has no
 * valid signature: words nobody vouches for are moderated again, and a row of
 * the model's that nobody vouches for is refused (`invalid_history`). A receipt
 * is opaque here. It rides with the thing it signs - a row, a file, a note, a
 * summary, the instructions, the snapshot - and is sent back with it; it fits
 * only those exact bytes, so editing the thing makes it a missing receipt and
 * there is nothing to clear.
 *
 * Old data has none: it loads and sends as before, its words are moderated once
 * and the receipts come back with the answer.
 */

/** The receipts of a request's instructions and snapshot, from what the browser holds. */
export function requestReceipts(personalization: StoredPersonalization, snapshotReceipt?: string | null): GuestReceipts | undefined {
  const out: GuestReceipts = {
    ...(personalization.aboutYouReceipt ? { aboutYou: personalization.aboutYouReceipt } : {}),
    ...(personalization.responseStyleReceipt ? { responseStyle: personalization.responseStyleReceipt } : {}),
    ...(snapshotReceipt ? { memorySnapshot: snapshotReceipt } : {}),
  }
  return Object.keys(out).length > 0 ? out : undefined
}

/**
 * A summary a request can carry. One the server did not sign (made before
 * receipts existed) is left out: it would be refused, and leaving it out costs
 * only that the model is sent the rows it summarised, which the server
 * compresses afresh if they do not fit.
 */
export function signedSummary(summary: ContextSummary | undefined): ContextSummary | undefined {
  return summary?.receipt ? summary : undefined
}

/**
 * The rows the last frame of a run has receipts for - the new question, and the
 * rows that went out without one - with them set, and the files among them.
 * Only the rows that change are returned.
 */
export function withEndReceipts(rows: StoredMessage[], receipts: GuestReceipts | undefined): StoredMessage[] {
  const messages = receipts?.messages
  const files = receipts?.files
  if (!messages && !files) return []
  const changed: StoredMessage[] = []
  for (const row of rows) {
    if (row.role !== 'user') continue
    let next = row
    const receipt = messages?.[row.id]
    if (receipt && row.receipt !== receipt) next = { ...next, receipt }
    if (files && row.attachments?.some((f) => files[f.id] && f.receipt !== files[f.id])) {
      next = { ...next, attachments: row.attachments.map((f) => (files[f.id] ? { ...f, receipt: files[f.id] } : f)) }
    }
    if (next !== row) changed.push(next)
  }
  return changed
}

/**
 * The notes of the list that the server signed in its last frame: its list
 * has the ids this one sent, so a note whose words are the same is the same
 * note, and takes the receipt. Returns the notes that change.
 */
export function adoptMemoryReceipts(list: Memory[], theirs: Memory[]): Memory[] {
  const server = new Map(theirs.map((m) => [m.id, m]))
  const changed: Memory[] = []
  for (const memory of list) {
    const signed = server.get(memory.id)
    if (signed?.receipt && signed.content === memory.content && memory.receipt !== signed.receipt) {
      changed.push({ ...memory, receipt: signed.receipt })
    }
  }
  return changed
}

/** The instructions' receipts after a run: the ones the last frame brought, else the ones held. */
export function withInstructionReceipts(held: StoredPersonalization, receipts: GuestReceipts | undefined): StoredPersonalization {
  if (!receipts) return held
  return {
    ...held,
    ...(receipts.aboutYou ? { aboutYouReceipt: receipts.aboutYou } : {}),
    ...(receipts.responseStyle ? { responseStyleReceipt: receipts.responseStyle } : {}),
  }
}
