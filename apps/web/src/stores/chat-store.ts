import type { Attachment, BatchEvent, Message, Run, SnapshotEvent } from '@openchat/api-client'
import { create } from 'zustand'

import { applyBatch, finishedBy, fromSnapshot, idle, rememberFinished, type ConversationLive } from '@/lib/run-state'
import type { Refusal } from '@/lib/moderation-refusal'
import type { Replacing } from '@/lib/transcript'

/**
 * Client state for the chat screen that is not in the query cache.
 *
 * `live` is each conversation's run in flight. It changes many times a second
 * while an answer streams, which would thrash the query cache, so it lives
 * here; once a run is over its rows are refetched through Query and the live
 * copy is let go (`settle`).
 */
/**
 * A message on its way to the server, shown in the transcript the moment it is
 * sent rather than a round trip later — on a slow link that round trip is the
 * difference between an app that answers the keyboard and one that does not.
 */
export interface OutboxItem {
  content: string
  /** The uploads sent with it, as the transcript draws them. */
  attachments: Attachment[]
}

interface ChatState {
  live: Record<string, ConversationLive>

  /**
   * Runs known to be over, newest last and bounded (`rememberFinished`). The
   * events stream can tell a run's whole story - started, finished, its rows
   * fetched, the live copy settled - before the HTTP response of the request
   * that started it comes back; that response, and a snapshot taken earlier
   * than the end, still say "running". A run does not go back, so neither is
   * allowed to write it into `live` again: nothing would ever finish it.
   */
  finished: readonly string[]

  /**
   * Each conversation's message being sent, until the server has taken it
   * (its run starts) or refused it. Keyed by conversation id, or by '' on the
   * empty screen between the send and the moment the page takes the conversation
   * it made for the message as its own (NextChat).
   */
  outbox: Record<string, OutboxItem>
  post: (key: string, item: OutboxItem) => void
  unpost: (key: string) => void

  /**
   * What the response that refused a send said about it (the strike warning),
   * by the id of the message the server kept for it. The message itself is in
   * the transcript - a `blocked` row, drawn with its reply card - and survives a
   * refresh; this is only the live warning, which a refresh forgets.
   */
  refusals: Record<string, Refusal>
  refuse: (messageId: string, refusal: Refusal) => void

  /**
   * Each conversation's redo — the last answer regenerated, or a question
   * edited (with the turns after it) — until the server has started the new
   * run: what is being deleted is hidden from the click on, not a round trip
   * later.
   */
  replacing: Record<string, Replacing>
  redo: (conversationId: string, target: Replacing | null) => void

  applySnapshot: (conversationId: string, snapshot: SnapshotEvent) => void
  applyBatch: (conversationId: string, batch: BatchEvent) => void
  /**
   * A run this page started, known from the POST before its first event.
   * `rows` are what the same commit wrote besides the run: the result of an
   * answered question.
   */
  startRun: (conversationId: string, run: Run, input: Message, rows?: Message[]) => void
  /** Drops a finished run once the message list holds its rows. */
  settle: (conversationId: string, runId: string) => void

  /**
   * The empty screen's temporary-chat switch: the conversation made for the
   * next message is temporary. Not persisted — a new visit starts an ordinary
   * chat, and a switch left on by mistake would quietly keep nothing.
   */
  temporaryChat: boolean
  setTemporaryChat: (on: boolean) => void
}

const without = <T,>(record: Record<string, T>, key: string) => {
  if (!(key in record)) return record
  const next = { ...record }
  delete next[key]
  return next
}

/** A run has started: what was waiting on it is the run's to show now. */
const settled = (state: ChatState, conversationId: string) => ({
  outbox: without(state.outbox, conversationId),
  replacing: without(state.replacing, conversationId),
})

export const useChatStore = create<ChatState>()((set) => ({
  live: {},
  finished: [],
  outbox: {},
  replacing: {},
  temporaryChat: false,

  setTemporaryChat: (temporaryChat) => set({ temporaryChat }),
  post: (key, item) => set((state) => ({ outbox: { ...state.outbox, [key]: item } })),
  refusals: {},
  refuse: (messageId, refusal) => set((state) => ({ refusals: { ...state.refusals, [messageId]: refusal } })),
  unpost: (key) => set((state) => ({ outbox: without(state.outbox, key) })),
  redo: (conversationId, target) =>
    set((state) => ({
      replacing: target ? { ...state.replacing, [conversationId]: target } : without(state.replacing, conversationId),
    })),

  applySnapshot: (conversationId, snapshot) =>
    set((state) => {
      const run = snapshot.run
      // Taken before the run ended and delivered after: out of date.
      if (run?.status === 'running' && state.finished.includes(run.id)) return state
      return {
        live: { ...state.live, [conversationId]: fromSnapshot(snapshot) },
        ...(run && run.status !== 'running' ? { finished: rememberFinished(state.finished, run.id) } : {}),
        // A run in flight already shows the message it answers.
        ...(run?.status === 'running' ? settled(state, conversationId) : {}),
      }
    }),

  applyBatch: (conversationId, batch) =>
    set((state) => {
      const starts = batch.events.some((event) => event.type === 'run_started')
      // Announcing a run that is already over (a replay, a late copy).
      if (starts && state.finished.includes(batch.runId)) return state
      const live = applyBatch(state.live[conversationId] ?? idle, batch)
      const ended = finishedBy(batch)
      return {
        live: { ...state.live, [conversationId]: live },
        ...(ended.length > 0 ? { finished: ended.reduce(rememberFinished, state.finished) } : {}),
        // The stream can announce the run before the POST that started it returns.
        ...(starts ? settled(state, conversationId) : {}),
      }
    }),

  startRun: (conversationId, run, input, rows = []) =>
    set((state) => {
      const current = state.live[conversationId]
      // The stream may already have announced it.
      if (current?.run?.run.id === run.id) return settled(state, conversationId)
      // Or told all of it, and had it handed over to the message list: the
      // response says "running" because that is what it was when it was written.
      if (state.finished.includes(run.id)) return settled(state, conversationId)
      return {
        ...settled(state, conversationId),
        live: {
          ...state.live,
          [conversationId]: { run: { run, messages: [input, ...rows], partial: null, retry: null, tools: {}, compacting: false } },
        },
      }
    }),

  settle: (conversationId, runId) =>
    set((state) => {
      const current = state.live[conversationId]
      if (current?.run?.run.id !== runId || current.run.run.status === 'running') return state
      return { live: { ...state.live, [conversationId]: idle }, finished: rememberFinished(state.finished, runId) }
    }),
}))

/**
 * Back to a blank page: the state of one person's (or one mode's) chat screen
 * must not be seen by the next. Used when the account or the guest/account mode
 * changes, and when the session ends.
 */
export function resetChatStore() {
  useChatStore.setState({ live: {}, finished: [], outbox: {}, replacing: {}, refusals: {}, temporaryChat: false })
}
