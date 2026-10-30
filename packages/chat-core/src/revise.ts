import type { Message } from '@openchat/api-client'

import { refusedTail, type Turn } from './transcript'

/**
 * What the composer does. Normally it sends a new message. While the last row of
 * the conversation is a message content moderation refused, the conversation
 * cannot go on until that message is reworded, so the composer is the editor of
 * it: what is sent from it is an edit of that row (the edit endpoint), not a new
 * message, and the page says so above it.
 *
 * Decided from the turns the page draws - the refused message is the last turn's
 * (`refusedTail`) - so a redo in flight, which hides the message, ends the mode
 * for as long as it lasts, and so does a run (its turn comes after).
 */
export type ComposerMode = { kind: 'send' } | { kind: 'revise'; message: Message }

export function composerMode(turns: Turn[]): ComposerMode {
  const message = refusedTail(turns)
  return message ? { kind: 'revise', message } : { kind: 'send' }
}

/**
 * Whether the composer should be filled with the refused message: the first time
 * the page sees this one and the composer is empty - after a refresh, on another
 * device, in another tab. (Refused from this composer, the words are in it
 * already, and nothing is touched.) `handled` is the refused message the page has
 * already dealt with, so that a person who emptied the field is not given the
 * words back at every render.
 *
 * Returns the text to put in the field, if any, and the message now handled.
 */
export function reviseDraft(
  mode: ComposerMode,
  input: string,
  handled: string | null,
): { fill: string | null; handled: string | null } {
  if (mode.kind !== 'revise' || mode.message.id === handled) return { fill: null, handled }
  return { fill: input.trim() === '' ? mode.message.content : null, handled: mode.message.id }
}
