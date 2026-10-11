import { useParams } from '@tanstack/react-router'
import { useCallback, useState } from 'react'

import { adopt, followAddress, openView, release } from '@/lib/chat-view'
import NextChat from './NextChat'

/**
 * The one chat page, for the new-chat screen and for every conversation.
 *
 * The routes (`_authed._chat`, `guest._chat`) draw this once and their children
 * are only addresses, so going from `/` to `/chat/<id>` does not remount it. What
 * does need a fresh page is opening another conversation (the sidebar, search,
 * back and forward, a deep link): the page is then keyed afresh, so nothing typed,
 * scrolled or chosen carries over. Sending the first message is not that: the
 * page makes the conversation itself and tells the view (`onCreated`), and the
 * address that follows names what is already shown. The rules are `lib/chat-view`.
 *
 * The same idea as Open WebUI's one `Chat.svelte`, which rewrites the address
 * with `history.replaceState` behind its router's back; here the router is told
 * (`navigate` with `replace`), and stays consistent.
 */
export default function ChatPage() {
  const params = useParams({ strict: false })
  const route = 'conversationId' in params ? params.conversationId : undefined

  const [view, setView] = useState(() => openView(route))
  // The address moved: follow it now, in this render, so the page is never
  // drawn with the old conversation under the new address.
  const current = followAddress(view, route)
  if (current !== view) setView(current)

  const onCreated = useCallback((id: string) => setView((held) => adopt(held, id)), [])
  const onRemoved = useCallback((id: string) => setView((held) => release(held, id)), [])

  return <NextChat key={current.epoch} conversationId={current.shown} onCreated={onCreated} onRemoved={onRemoved} />
}
