import { createFileRoute } from '@tanstack/react-router'

import ChatPage from '@/components/chat/ChatPage'

/**
 * The chat itself: one page for the new-chat screen (`/`) and for every
 * conversation (`/chat/$conversationId`). It is a layout that draws the page
 * and none of its children - they are only addresses, and carry the loader that
 * starts a conversation's queries early - so moving between them does not
 * remount it. Sending the first message makes the conversation and changes the
 * address under the same page; opening another conversation starts it over
 * (ChatPage).
 */
export const Route = createFileRoute('/_authed/_chat')({
  component: ChatPage,
})
