import type { BatchEvent, Message, Run, SnapshotEvent } from '@openchat/api-client'
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

interface ChatState {
  live: Record<string, ConversationLive>

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

export const useChatStore = create<ChatState>()((set, get) => ({
  live: {},
  pendingPrompt: null,

  applySnapshot: (conversationId, snapshot) =>
    set((state) => ({ live: { ...state.live, [conversationId]: fromSnapshot(snapshot) } })),

  applyBatch: (conversationId, batch) =>
    set((state) => ({
      live: { ...state.live, [conversationId]: applyBatch(state.live[conversationId] ?? idle, batch) },
    })),

  startRun: (conversationId, run, input) =>
    set((state) => {
      const current = state.live[conversationId]
      // The stream may already have announced it.
      if (current?.run?.run.id === run.id) return state
      return {
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
