import { isOpenChatError } from '@openchat/api-client'

import { refusalOf } from '@/lib/moderation-refusal'

/**
 * What a failed first send of a new chat says about the conversation made for
 * it, and about the files it carried.
 *
 * The conversation is made for that one message. Refused by the server before
 * anything was written (a limit, no model, a bad file), it is empty and goes -
 * the text and the files go back to the empty screen. A message content
 * moderation refused is the exception: the server kept it in the conversation,
 * which stays. Any other failure (a dropped connection, a 5xx) may have written
 * the message after all, so the conversation stays too.
 */
export function startWroteNothing(error: unknown): boolean {
  return (
    isOpenChatError(error) &&
    error.status >= 400 &&
    error.status < 500 &&
    // A signed-out account is taken to sign-in by the page, which has its own business with the draft.
    error.status !== 401 &&
    !refusalOf(error)?.messageId
  )
}

/**
 * The files to give back to the composer after a failed send: the ones that
 * were uploaded and are still unclaimed (they stay valid for a day). Not those
 * the server says it cannot use - never uploaded, already sent, or not the
 * sender's - which would only be refused again.
 */
export function returnableFiles<T>(error: unknown, files: T[]): T[] {
  return isOpenChatError(error) && error.code === 'attachment_unavailable' ? [] : files
}
