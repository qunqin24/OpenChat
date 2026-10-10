import type { Attachment, BatchEvent, Message, Run, SnapshotEvent } from '@openchat/api-client'
import { create } from 'zustand'

import { applyBatch, fromSnapshot, idle, type ConversationLive } from '@/lib/run-state'

/**
 * Client state for the chat screen that is not in the query cache.
 *
 * `live` is each conversation's run in flight. It changes many times a second
 * while an answer streams, which would thrash the query cache, so it lives
 * here; once a run is over its rows are refetched through Query and the live
 * copy is let go (`settle`).
 */
/** What was written on the empty-state screen, to be sent once the conversation exists. */
export interface PendingPrompt {
  content: string
  /** Uploads already made for it; the new route sends them with the text. */
  attachmentIds: string[]
}

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
   * Each conversation's message being sent, until the server has taken it
   * (its run starts) or refused it. Keyed by conversation id, or by '' on the
   * empty screen before the conversation exists.
   */
  outbox: Record<string, OutboxItem>
  post: (key: string, item: OutboxItem) => void
  unpost: (key: string) => void

  /**
   * Prompt typed on the empty-state screen, carried across the navigation that
   * follows creating the conversation for it. The new route consumes it on mount.
   */
  pendingPrompt: PendingPrompt | null

  applySnapshot: (conversationId: string, snapshot: SnapshotEvent) => void
  applyBatch: (conversationId: string, batch: BatchEvent) => void
  /** A run this page started, known from the POST before its first event. */
  startRun: (conversationId: string, run: Run, input: Message) => void
  /** Drops a finished run once the message list holds its rows. */
  settle: (conversationId: string, runId: string) => void

  setPendingPrompt: (prompt: PendingPrompt | null) => void
  takePendingPrompt: () => PendingPrompt | null
}

const without = (outbox: Record<string, OutboxItem>, key: string) => {
  if (!(key in outbox)) return outbox
  const next = { ...outbox }
  delete next[key]
  return next
}

export const useChatStore = create<ChatState>()((set, get) => ({
  live: {},
  outbox: {},
  pendingPrompt: null,

  post: (key, item) => set((state) => ({ outbox: { ...state.outbox, [key]: item } })),
  unpost: (key) => set((state) => ({ outbox: without(state.outbox, key) })),

  applySnapshot: (conversationId, snapshot) =>
    set((state) => ({
      live: { ...state.live, [conversationId]: fromSnapshot(snapshot) },
      // A run in flight already shows the message it answers.
      outbox: snapshot.run?.status === 'running' ? without(state.outbox, conversationId) : state.outbox,
    })),

  applyBatch: (conversationId, batch) =>
    set((state) => ({
      live: { ...state.live, [conversationId]: applyBatch(state.live[conversationId] ?? idle, batch) },
      // The stream can announce the run before the POST that started it returns.
      outbox: batch.events.some((event) => event.type === 'run_started')
        ? without(state.outbox, conversationId)
        : state.outbox,
    })),

  startRun: (conversationId, run, input) =>
    set((state) => {
      const current = state.live[conversationId]
      const outbox = without(state.outbox, conversationId)
      // The stream may already have announced it.
      if (current?.run?.run.id === run.id) return { outbox }
      return {
        outbox,
        live: {
          ...state.live,
          [conversationId]: { run: { run, messages: [input], partial: null, retry: null, tools: {} } },
        },
      }
    }),

  settle: (conversationId, runId) =>
    set((state) => {
      const current = state.live[conversationId]
      if (current?.run?.run.id !== runId || current.run.run.status === 'running') return state
      return { live: { ...state.live, [conversationId]: idle } }
    }),

  setPendingPrompt: (pendingPrompt) => set({ pendingPrompt }),

  takePendingPrompt: () => {
    const { pendingPrompt } = get()
    if (pendingPrompt !== null) set({ pendingPrompt: null })
    return pendingPrompt
  },
}))
