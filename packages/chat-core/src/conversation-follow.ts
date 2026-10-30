import { isOpenChatError, type CompactionState, type ContextSummary, type ConversationEvent, type MessagePage } from '@openchat/api-client'
import type { QueryClient } from '@tanstack/query-core'
import type { StoreApi } from 'zustand'

import type { ChatState } from './chat-store'
import { refreshConfirmed, refreshThenSettle } from './hand-over'
import { CAPACITY_CODE } from './model-capacity'
import { queryKeys } from './query-keys'
import { spliceNewest, truncateTranscript, type TranscriptPages } from './transcript-pages'

/** Reconnect delays, in ms: quick at first, then backing off to a ceiling. */
export const RECONNECT_DELAYS = [500, 1000, 2000, 5000, 10000, 15000]

/**
 * Brings the newest rows up to date — what a finished run wrote — without
 * downloading the older pages again. They are immutable, so the fresh newest
 * page is spliced onto what is already held: rows older than it stay, and the
 * reader's revealed history (and the scroll position it holds up) does not
 * move. Rows that were deleted meanwhile (`spliceNewest`) are gone from the
 * result.
 */
export async function refreshNewestMessages(
  queryClient: QueryClient,
  conversationId: string,
  fetchNewest: () => Promise<MessagePage>,
) {
  const fresh = await fetchNewest()
  queryClient.setQueryData<TranscriptPages>(queryKeys.messages.list(conversationId), (held) => spliceNewest(held, fresh))
}

/** Puts a summary — or none — where the transcript reads it. */
export function setCompaction(queryClient: QueryClient, conversationId: string, summary: ContextSummary | undefined) {
  queryClient.setQueryData<CompactionState>(queryKeys.conversations.compaction(conversationId), { summary })
}

export interface FollowOptions {
  conversationId: string
  /** Aborting it stops following; following aborts it itself when the conversation is gone. */
  controller: AbortController
  /** The conversation's events (`streamConversationEvents`), as this app connects. */
  events: (signal: AbortSignal) => AsyncIterable<ConversationEvent>
  queryClient: QueryClient
  store: StoreApi<ChatState>
  /** The newest page of the transcript, fetched afresh. */
  fetchNewest: () => Promise<MessagePage>
  /** The conversation is gone (deleted elsewhere, or never existed): show a new chat. */
  leave: () => void
  /** The session ended: say so once (the app signs out) and stop. */
  unauthorized: (error: unknown) => void
  wait: (ms: number) => Promise<void>
}

/**
 * Follows a conversation's runs for as long as the screen is open.
 *
 * Opening the page attaches to whatever is in flight — a run started in
 * another tab, or one that kept going while this page was closed — and a
 * dropped connection reattaches from a fresh snapshot. A run that finishes
 * hands over to Query: the newest page of the message list is refetched (the
 * older pages the reader has opened are immutable and stay), and only then is
 * the live copy dropped, so the answer never disappears in between.
 *
 * Rows deleted by a redo - a question edited, an answer regenerated, here or in
 * another tab - are announced as `transcript_changed`, in the batch that starts
 * the run that follows. The newest page is fetched again then too, so the
 * transcript here drops what is gone; and the rows the event or this page's own
 * redo names are out of the cache before the batch is applied, so they are not
 * drawn beside the run that replaces them in between.
 *
 * The conversation's compressed context rides on the same stream: a run
 * announces the summary it made (`compaction_finished`), a person taking it back
 * is announced outside any run (`compaction_changed`), and rows that were
 * deleted may have taken the summary with them, so it is read again with the
 * transcript (`transcript_changed`).
 *
 * A conversation the server says does not exist — deleted from another tab or
 * device while this page was open — is not a dropped connection: there is
 * nothing to reattach to, so following stops and the page goes to a new chat.
 *
 * The web app and the mobile app both run this; each passes how it connects
 * and where it goes.
 */
export async function followConversation(options: FollowOptions): Promise<void> {
  const { conversationId, controller, queryClient, store, wait } = options
  const { applySnapshot, applyBatch, settle } = store.getState()
  const invalidate = (queryKey: readonly unknown[]) => void queryClient.invalidateQueries({ queryKey })

  // Brings the message list up to date: its newest page only, spliced onto
  // the older ones already held. If that fails, the plain refetch of every
  // page is the fallback. True only when one of them actually worked - the
  // fallback invalidating a query whose refetch then fails is not a refresh.
  const refreshMessages = () =>
    refreshConfirmed({
      refreshNewest: () => refreshNewestMessages(queryClient, conversationId, options.fetchNewest),
      refetchAll: () =>
        queryClient.refetchQueries({ queryKey: queryKeys.messages.list(conversationId) }, { throwOnError: true }),
    })

  // Refreshes the list and, only once that is confirmed, lets the live copy
  // of `runId` go (see hand-over.ts). A refresh that fails leaves the answer
  // on screen and is tried again; a later success settles it.
  const catchUp = (runId?: string, settleRun: (id: string) => void = (id) => settle(conversationId, id)) =>
    refreshThenSettle({
      refresh: refreshMessages,
      settle: settleRun,
      runId,
      wait,
      cancelled: () => controller.signal.aborted,
    })

  // Hands a finished run over to the query cache.
  const handOver = (runId: string) => {
    void catchUp(runId)
    // The conversation's position in the list and its model may have moved.
    invalidate(queryKeys.conversations.all)
    // The model may have kept a note about the person while it ran.
    invalidate(queryKeys.memory.all)
    // And the run may have compressed the conversation, which a page that
    // joined it half way did not see happen.
    invalidate(queryKeys.conversations.compaction(conversationId))
  }

  let attempt = 0
  while (!controller.signal.aborted) {
    try {
      for await (const event of options.events(controller.signal)) {
        attempt = 0
        if (event.type === 'snapshot') {
          const held = store.getState().live[conversationId]?.run
          if (held && !event.run) {
            // The run we were showing ended while we were away: its rows are
            // only in the message list now. It stays on screen until the
            // list holds them; then the snapshot (nothing running) replaces
            // it - unless another run has taken its place meanwhile.
            const replace = (id: string) => {
              if (store.getState().live[conversationId]?.run?.run.id === id) applySnapshot(conversationId, event)
            }
            void catchUp(held.run.id, replace).then((done) => {
              // Still showing a run the server says is over, and the list
              // cannot be read: better a gap than a Stop button that never goes.
              if (!done && held.run.status === 'running') replace(held.run.id)
            })
            continue
          }
          applySnapshot(conversationId, event)
          // Another run than the one held: the one held finished while we
          // were away, and its rows are only in the list now. Or the same
          // run, over, after a hand-over that never succeeded.
          if (held && event.run) {
            if (held.run.id !== event.run.id) void catchUp()
            else if (event.run.status !== 'running') void catchUp(event.run.id)
          }
          continue
        }
        const changed = event.events.find((e) => e.type === 'transcript_changed')
        if (changed) {
          // What is gone: what the event says (an edited question and what
          // followed it), or the redo this page asked for. Read before the
          // batch is applied: the run starting ends the redo.
          const redo = changed.from ? { from: changed.from } : store.getState().replacing[conversationId]
          if (redo) {
            queryClient.setQueryData<TranscriptPages>(queryKeys.messages.list(conversationId), (held) =>
              truncateTranscript(held, redo),
            )
          }
        }
        applyBatch(conversationId, event)
        for (const runEvent of event.events) {
          // Deleted, here or in another tab or on another device: there is
          // nothing left to show, and reconnecting would only be refused.
          if (runEvent.type === 'conversation_deleted') {
            controller.abort()
            invalidate(queryKeys.conversations.all)
            options.leave()
            return
          }
          if (runEvent.type === 'run_finished') {
            handOver(runEvent.run.id)
            // Stopped because the model reached its cap: the picker is
            // behind, and should show the model as unavailable.
            if (runEvent.run.errorCode === CAPACITY_CODE) invalidate(queryKeys.models.all)
            // The composer's meter reads the request that run just made.
            invalidate(queryKeys.conversations.context(conversationId))
          }
          if (runEvent.type === 'run_started') {
            // Started elsewhere, or by us: either way the list order moved,
            // and the list marks it as working until it is done.
            invalidate(queryKeys.conversations.list())
            invalidate(queryKeys.conversations.running())
          }
          // Titled after its first answer. The batch carries the finished
          // run's last version, so the run state above has already seen
          // past it; the title is applied here, outside it.
          if (runEvent.type === 'title_changed') invalidate(queryKeys.conversations.all)
          // The summary a run made is the conversation's now; a failed
          // compression (no summary) changes nothing but the status line.
          if (runEvent.type === 'compaction_finished' && runEvent.summary) {
            setCompaction(queryClient, conversationId, runEvent.summary)
          }
          // A sub-agent started, took a step or ended; or a report of one was
          // delivered, which changes how it stands.
          if (
            runEvent.type === 'subagents_changed' ||
            (runEvent.type === 'message_appended' && runEvent.message.origin === 'subagent') ||
            (runEvent.type === 'run_started' && runEvent.message.origin === 'subagent')
          ) {
            invalidate(queryKeys.conversations.subagents(conversationId))
          }
          // Taken back by a person, here or elsewhere.
          if (runEvent.type === 'compaction_changed') {
            setCompaction(queryClient, conversationId, runEvent.summary)
            invalidate(queryKeys.conversations.context(conversationId))
          }
          // Rows were deleted - a question edited or an answer regenerated,
          // here or elsewhere. The newest page is what changed, so that is
          // what is fetched (and spliced onto the older pages that still
          // hold): never the whole key, which would download every page the
          // reader had opened again.
          if (runEvent.type === 'transcript_changed') {
            // A summary that ended at a deleted row went with it.
            invalidate(queryKeys.conversations.compaction(conversationId))
            // A finished run still held live would put its own rows back over
            // the transcript now (settle leaves a running one be).
            void catchUp(store.getState().live[conversationId]?.run?.run.id)
          }
        }
      }
    } catch (error) {
      if (controller.signal.aborted) return
      if (isOpenChatError(error) && error.status === 404) {
        invalidate(queryKeys.conversations.all)
        options.leave()
        return
      }
      // The session ended (or never was): reconnecting would be refused the
      // same way for ever. Say so once and stop.
      if (isOpenChatError(error) && error.status === 401) {
        options.unauthorized(error)
        return
      }
    }
    if (controller.signal.aborted) return
    const delay = RECONNECT_DELAYS[Math.min(attempt, RECONNECT_DELAYS.length - 1)]!
    attempt++
    await wait(delay)
  }
}
