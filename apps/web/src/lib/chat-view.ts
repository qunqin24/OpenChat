import type { HistoryState } from '@tanstack/react-router'

declare module '@tanstack/react-router' {
  interface HistoryState {
    /** Set afresh by every "new chat" (newChatState). */
    newChat?: number
  }
}

// The chat page's view rules are chat-core's (chat-view); this is the web
// router's half: how a "new chat" is told apart in history.

let nonce = 0

/**
 * The history state of a "new chat" link or navigation: a mark that differs
 * every time, so asking for a new chat is a navigation even from `/`, and the
 * chat page sees it (ChatPage, startOver).
 */
export function newChatState(previous: HistoryState): HistoryState {
  return { ...previous, newChat: Date.now() * 1000 + (nonce++ % 1000) }
}
