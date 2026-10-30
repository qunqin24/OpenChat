import { isOpenChatError, type Message, type ReasoningEffort } from '@openchat/api-client'
import type { QueryClient } from '@tanstack/query-core'
import type { StoreApi } from 'zustand'

import type { ChatState } from './chat-store'
import type { Refusal } from './moderation-refusal'
import { queryKeys } from './query-keys'
import { appendRow, truncateTranscript, type TranscriptPages } from './transcript-pages'

// Sending, as both apps do it: the request bodies, the cache moves around a
// send, and what a failure is called. The mutations themselves are each app's
// (they are React hooks), and stay thin over these.

/** Server error codes a send can fail with that have their own wording (`chat.send-error.*`). */
export const SEND_ERRORS = [
  'conversation_busy',
  'no_models',
  'model_unavailable',
  'model_capacity_reached',
  'content_blocked',
  'revise_refused',
  'attachments_not_editable',
  'references_not_editable',
  'reference_unavailable',
  'reference_empty',
  'too_many_references',
  'moderation_unavailable',
  'message_too_large_for_moderation',
  'too_many_runs',
  'too_many_attachments',
  'attachments_too_large',
  'attachment_unsupported',
  'attachment_unavailable',
  'attachment_unreadable',
  'storage_quota_exceeded',
  'attachment_text_too_long',
  'nothing_to_answer',
  'not_latest',
  'message_not_found',
  'not_pending',
  'invalid_answers',
] as const

export type SendErrorCode = (typeof SEND_ERRORS)[number]

/** The locale key for a failed send: its own wording if it has one, else the generic one. */
export function sendErrorKey(error: unknown): `chat.send-error.${SendErrorCode | 'generic'}` {
  const code = isOpenChatError(error) ? error.code : ''
  const known = SEND_ERRORS.find((c) => c === code)
  return `chat.send-error.${known ?? 'generic'}`
}

/** What a run is started with, as the composer has it set. */
export interface RunChoice {
  model?: string
  reasoningEffort?: ReasoningEffort
  webSearch?: boolean
}

/** The choice as a request body has it: only what was set. */
export const choiceBody = ({ model, reasoningEffort, webSearch }: RunChoice) => ({
  ...(model ? { model } : {}),
  ...(reasoningEffort ? { reasoningEffort } : {}),
  ...(webSearch !== undefined ? { webSearch } : {}),
})

/**
 * Takes the rows a redo deleted out of the cached transcript, for the moment
 * between the server having done it and the fetch that shows the same. The
 * events stream gets there first when it can (conversation-follow.ts).
 */
export function forgetDeleted(queryClient: QueryClient, conversationId: string, target: { from?: string; after?: string }) {
  queryClient.setQueryData<TranscriptPages>(queryKeys.messages.list(conversationId), (held) =>
    truncateTranscript(held, target),
  )
}

/**
 * Shows a refused send as the message the server kept: the warning its response
 * carried (live only), the row in the cached transcript, and the newest page
 * fetched to replace that row with the server's (`refreshNewest`). The fetch
 * failing is no harm: the events stream announces the kept message
 * (`transcript_changed`) and fetches it with retries.
 */
export function keepRefused(
  queryClient: QueryClient,
  store: StoreApi<ChatState>,
  conversationId: string,
  refusal: Refusal,
  content: string,
  refreshNewest: () => Promise<void>,
) {
  const messageId = refusal.messageId!
  store.getState().refuse(messageId, refusal)
  const row: Message = {
    id: messageId,
    conversationId,
    role: 'user',
    content,
    parts: [{ type: 'text', text: content }],
    status: 'blocked',
    createdAt: new Date().toISOString(),
  }
  queryClient.setQueryData<TranscriptPages>(queryKeys.messages.list(conversationId), (held) => appendRow(held, row))
  void refreshNewest().catch(() => undefined)
}
