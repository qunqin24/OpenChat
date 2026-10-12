import { createFileRoute } from '@tanstack/react-router'

import ChatPage from '@/components/chat/ChatPage'

/** The guest's chat: one page for the new-chat screen and every conversation, as the account's (`_authed._chat.tsx`). */
export const Route = createFileRoute('/guest/_chat')({
  component: ChatPage,
})
