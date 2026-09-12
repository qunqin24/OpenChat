import { create } from 'zustand'

/**
 * Live state for the in-flight assistant reply. It is deliberately separate from
 * TanStack Query: the persisted message list is server state, while a streaming
 * reply is ephemeral client state that changes many times per second.
 */
interface StreamState {
  conversationId: string
  messageId: string | null
  text: string
  error: string | null
}

interface ChatState {
  stream: StreamState | null
  controller: AbortController | null
  /**
   * Prompt typed on the empty-state screen, carried across the navigation that
   * follows creating the conversation for it. The new route consumes it on mount.
   */
  pendingPrompt: string | null

  /** Begins a stream, aborting any previous one. Returns the signal to pass to fetch. */
  start: (conversationId: string) => AbortSignal
  setMessageId: (messageId: string) => void
  appendDelta: (delta: string) => void
  fail: (message: string) => void
  finish: () => void
  /** Aborts the in-flight stream, keeping whatever text already arrived. */
  stop: () => void

  setPendingPrompt: (prompt: string | null) => void
  takePendingPrompt: () => string | null

  isStreaming: (conversationId: string) => boolean
}

export const useChatStore = create<ChatState>()((set, get) => ({
  stream: null,
  controller: null,
  pendingPrompt: null,

  setPendingPrompt: (pendingPrompt) => set({ pendingPrompt }),

  takePendingPrompt: () => {
    const { pendingPrompt } = get()
    if (pendingPrompt !== null) set({ pendingPrompt: null })
    return pendingPrompt
  },

  start: (conversationId) => {
    get().controller?.abort()
    const controller = new AbortController()
    set({
      controller,
      stream: { conversationId, messageId: null, text: '', error: null },
    })
    return controller.signal
  },

  setMessageId: (messageId) =>
    set((state) => (state.stream ? { stream: { ...state.stream, messageId } } : state)),

  appendDelta: (delta) =>
    set((state) =>
      state.stream ? { stream: { ...state.stream, text: state.stream.text + delta } } : state,
    ),

  fail: (message) =>
    set((state) => ({
      controller: null,
      stream: state.stream ? { ...state.stream, error: message } : state.stream,
    })),

  finish: () => set({ stream: null, controller: null }),

  stop: () => {
    get().controller?.abort()
    set({ controller: null })
  },

  isStreaming: (conversationId) => {
    const { stream, controller } = get()
    return controller !== null && stream?.conversationId === conversationId
  },
}))
