import { isOpenChatError } from '@openchat/api-client'

/**
 * A message the server's content moderation refused (422 `content_blocked`),
 * as the page shows it: a card in the conversation and a toast. Nothing of the
 * message was written, so the card lives only in the page (chat store) until
 * the next send or until it is closed.
 */
export interface Refusal {
  /** What was sent, shown as the person's message above the card. */
  content?: string
  /** More blocked messages within `windowHours` that bring the sanction; absent when none will. */
  strikesLeft?: number
  windowHours?: number
}

export const CONTENT_BLOCKED = 'content_blocked'

/** The refusal an error is, or null when it is not one. `content` is the text that was refused. */
export function refusalOf(error: unknown, content?: string): Refusal | null {
  if (!isOpenChatError(error) || error.code !== CONTENT_BLOCKED) return null
  const left = error.details?.strikesLeft
  const hours = error.details?.windowHours
  if (typeof left === 'number' && left > 0 && typeof hours === 'number' && hours > 0) {
    return { ...(content !== undefined ? { content } : {}), strikesLeft: left, windowHours: hours }
  }
  return content !== undefined ? { content } : {}
}
