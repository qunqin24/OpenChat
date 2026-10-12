import { useParams, useRouterState } from '@tanstack/react-router'
import { useCallback, useLayoutEffect, useState } from 'react'

import { useChatStore } from '@/stores/chat-store'
import { adopt, followAddress, openView, release, startOver } from '@/lib/chat-view'
import ChatView from './ChatView'

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
  // Every "new chat" leaves a fresh mark in the history state (newChatState).
  const asked = useRouterState({ select: (state) => state.location.state.newChat })
  const [seen, setSeen] = useState(asked)
  // The address moved: follow it now, in this render, so the page is never
  // drawn with the old conversation under the new address.
  let current = followAddress(view, route)
  if (asked !== seen) {
    setSeen(asked)
    // A new chat asked for at `/` itself, while the page shows the conversation
    // its first message made: the address has not caught up, so only this says so.
    // Only a mark that appears is an ask. One that goes is the address moving
    // on (to the conversation, which carries none) - and the router updates the
    // location a render before the params, so in that render the route still
    // reads `/`: taking it as an ask would start the page over under the
    // conversation it just made.
    if (asked !== undefined && route === undefined) current = startOver(current, useChatStore.getState().outbox[''] !== undefined)
  }
  if (current !== view) setView(current)

  // A page that starts over does not inherit a first message still on the empty
  // key from the page before it (its send goes on in the background, under its
  // conversation's own key once that is made). Before paint, so it is never drawn.
  useLayoutEffect(() => {
    if (current.epoch > 0) useChatStore.getState().unpost('')
  }, [current.epoch])

  const onCreated = useCallback((id: string) => setView((held) => adopt(held, id)), [])
  const onRemoved = useCallback((id: string) => setView((held) => release(held, id)), [])

  return <ChatView key={current.epoch} conversationId={current.shown} onCreated={onCreated} onRemoved={onRemoved} />
}
