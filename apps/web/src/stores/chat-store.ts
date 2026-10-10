import type { Attachment, BatchEvent, Message, Run, SnapshotEvent } from '@openchat/api-client'
import { create } from 'zustand'

import { applyBatch, fromSnapshot, idle, type ConversationLive } from '@/lib/run-state'
import type { Replacing } from '@/lib/transcript'

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
   * Each conversation's redo — the last answer regenerated, or a question
   * edited (with the turns after it) — until the server has started the new
   * run: what is being deleted is hidden from the click on, not a round trip
   * later.
   */
  replacing: Record<string, Replacing>
  redo: (conversationId: string, target: Replacing | null) => void

  /**
   * Prompt typed on the empty-state screen, carried across the navigation that
   * follows creating the conversation for it. The new route consumes it on mount.
   */
  pendingPrompt: PendingPrompt | null

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

  setPendingPrompt: (prompt: PendingPrompt | null) => void
  takePendingPrompt: () => PendingPrompt | null
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

export const useChatStore = create<ChatState>()((set, get) => ({
  live: {},
  outbox: {},
  replacing: {},
  pendingPrompt: null,
  temporaryChat: false,

  setTemporaryChat: (temporaryChat) => set({ temporaryChat }),
  post: (key, item) => set((state) => ({ outbox: { ...state.outbox, [key]: item } })),
  unpost: (key) => set((state) => ({ outbox: without(state.outbox, key) })),
  redo: (conversationId, target) =>
    set((state) => ({
      replacing: target ? { ...state.replacing, [conversationId]: target } : without(state.replacing, conversationId),
    })),

  applySnapshot: (conversationId, snapshot) =>
    set((state) => ({
      live: { ...state.live, [conversationId]: fromSnapshot(snapshot) },
      // A run in flight already shows the message it answers.
      ...(snapshot.run?.status === 'running' ? settled(state, conversationId) : {}),
    })),

  applyBatch: (conversationId, batch) =>
    set((state) => ({
      live: { ...state.live, [conversationId]: applyBatch(state.live[conversationId] ?? idle, batch) },
      // The stream can announce the run before the POST that started it returns.
      ...(batch.events.some((event) => event.type === 'run_started') ? settled(state, conversationId) : {}),
    })),

  startRun: (conversationId, run, input, rows = []) =>
    set((state) => {
      const current = state.live[conversationId]
      // The stream may already have announced it.
      if (current?.run?.run.id === run.id) return settled(state, conversationId)
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
      return { live: { ...state.live, [conversationId]: idle } }
    }),

  setPendingPrompt: (pendingPrompt) => set({ pendingPrompt }),

  takePendingPrompt: () => {
    const { pendingPrompt } = get()
    if (pendingPrompt !== null) set({ pendingPrompt: null })
    return pendingPrompt
  },
}))
