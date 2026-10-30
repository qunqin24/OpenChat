import { router, useLocalSearchParams, useNavigation } from 'expo-router'
import type { DrawerNavigationProp } from '@react-navigation/drawer'
import type { ParamListBase } from '@react-navigation/native'
import { useCallback, useEffect, useLayoutEffect, useState } from 'react'

import { adopt, followAddress, openView, release, startOver } from '@openchat/chat-core/chat-view'

import { ChatView } from '@/components/chat/ChatView'
import { useShownConversation } from '@/lib/shown'
import { useChatStore } from '@/lib/store'

/**
 * The one chat screen, for the new-chat screen and every conversation (the
 * web's ChatPage). Its address is `?id=` (the conversation) and `?n=` (a new
 * chat asked for); the rules for when the view starts over and when it keeps
 * what it shows are chat-core/chat-view.ts. Sending the first message makes
 * the conversation here, and the address follows once the server has it.
 */
export default function ChatScreen() {
  const { id, n } = useLocalSearchParams<{ id?: string; n?: string }>()
  const navigation = useNavigation<DrawerNavigationProp<ParamListBase>>()
  const route = id || undefined

  const [view, setView] = useState(() => openView(route))
  const [seen, setSeen] = useState(n)
  let current = followAddress(view, route)
  if (n !== seen) {
    setSeen(n)
    if (n !== undefined && route === undefined) current = startOver(current, useChatStore.getState().outbox[''] !== undefined)
  }
  if (current !== view) setView(current)

  // A view that starts over does not inherit a first message still on the
  // empty key from the one before it; before paint, so it is never drawn.
  useLayoutEffect(() => {
    if (current.epoch > 0) useChatStore.getState().unpost('')
  }, [current.epoch])

  const setShown = useShownConversation((s) => s.set)
  useEffect(() => setShown(current.shown), [current.shown, setShown])

  const onCreated = useCallback((made: string) => setView((held) => adopt(held, made)), [])
  const onRemoved = useCallback((made: string) => setView((held) => release(held, made)), [])
  const newChat = useCallback(() => router.setParams({ id: undefined, n: String(Date.now()) }), [])

  return (
    <ChatView
      key={current.epoch}
      conversationId={current.shown}
      onCreated={onCreated}
      onRemoved={onRemoved}
      onSettled={(made) => router.setParams({ id: made, n: undefined })}
      onGone={newChat}
      onOpenDrawer={() => navigation.openDrawer()}
      onNewChat={newChat}
    />
  )
}
