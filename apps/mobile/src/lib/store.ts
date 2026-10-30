import { create } from 'zustand'

import { chatStateCreator, initialChatState, type ChatState } from '@openchat/chat-core/chat-store'

/** Live runs, the outbox and redos (chat-core/chat-store.ts), this app's own copy. */
export const useChatStore = create<ChatState>()(chatStateCreator)

/** Forgets everything the last account had going, on signing out or in as someone else. */
export function resetChatStore() {
  useChatStore.setState(initialChatState)
}
