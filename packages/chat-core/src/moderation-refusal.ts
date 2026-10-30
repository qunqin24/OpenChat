import { isOpenChatError } from '@openchat/api-client'

/**
 * A message the server's content moderation refused (422 `content_blocked`),
 * as the page shows it: a toast, and - for a send - the message itself, which
 * the server keeps as a `blocked` row in the conversation and the page draws as
 * the person's message with a reply card under it. Only what the response says
 * is held here: the warning is live, from this response; after a refresh the
 * card shows without it.
 */
export interface Refusal {
  /** The message the server kept (a refused send); absent for an edit or an answer, which keep nothing. */
  messageId?: string
  /** More blocked messages within `windowHours` that bring the sanction; absent when none will. */
  strikesLeft?: number
  windowHours?: number
}

export const CONTENT_BLOCKED = 'content_blocked'

/** The refusal an error is, or null when it is not one. */
export function refusalOf(error: unknown): Refusal | null {
  if (!isOpenChatError(error) || error.code !== CONTENT_BLOCKED) return null
  const out: Refusal = {}
  const kept = error.details?.messageId
  if (typeof kept === 'string' && kept !== '') out.messageId = kept
  const left = error.details?.strikesLeft
  const hours = error.details?.windowHours
  if (typeof left === 'number' && left > 0 && typeof hours === 'number' && hours > 0) {
    out.strikesLeft = left
    out.windowHours = hours
  }
  return out
}

/**
 * Why saving the person's own text - custom instructions, a memory - failed when
 * content moderation was the reason: the text was refused (422 `content_blocked`,
 * with the same strike warning a refused message carries), or the check could not
 * be made and the setting says not to go on without it (503 `moderation_unavailable`).
 * Null for any other failure. Nothing was saved either way, and the page keeps
 * what the person typed.
 */
export type SaveRefusal = ({ kind: 'blocked' } & Refusal) | { kind: 'unavailable' }

export function saveRefusalOf(error: unknown): SaveRefusal | null {
  const refusal = refusalOf(error)
  if (refusal) return { kind: 'blocked', ...refusal }
  if (isOpenChatError(error) && error.code === 'moderation_unavailable') return { kind: 'unavailable' }
  return null
}
