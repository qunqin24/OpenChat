import { useNavigate } from '@tanstack/react-router'
import { ArrowDown } from 'lucide-react'
import { useCallback, useEffect, useLayoutEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { useConversation, useCreateConversation } from '@/hooks/use-conversations'
import { useMessages } from '@/hooks/use-messages'
import { useSendMessage } from '@/hooks/use-send-message'
import { useChatStore } from '@/stores/chat-store'
import Composer from './Composer'
import MessageItem from './MessageItem'
import { SidebarToggle } from './NextShell'
import Welcome from './Welcome'

/** Distance from the bottom, in px, still counted as "parked at the tail". */
const BOTTOM_SLACK = 24

/** Placeholder until the backend serves a provider catalogue. */
const MODEL_NAME = 'OpenChat'

interface NextChatProps {
  conversationId?: string
}

export function NextChat({ conversationId }: NextChatProps) {
  const { t } = useTranslation()
  const navigate = useNavigate()

  const [input, setInput] = useState('')
  const scrollRef = useRef<HTMLDivElement>(null)
  const parked = useRef(true)
  const [showJump, setShowJump] = useState(false)

  const conversation = useConversation(conversationId ?? '')
  const messages = useMessages(conversationId ?? '')
  const createConversation = useCreateConversation()
  const sendMessage = useSendMessage(conversationId ?? '')

  const stream = useChatStore((s) => s.stream)
  const stop = useChatStore((s) => s.stop)
  const setPendingPrompt = useChatStore((s) => s.setPendingPrompt)
  const takePendingPrompt = useChatStore((s) => s.takePendingPrompt)

  const working = stream !== null && stream.conversationId === conversationId

  // The server persists the assistant row before the first token, so rows still
  // marked `streaming` are hidden here and rendered by the live bubble instead.
  const persisted = (messages.data?.items ?? []).filter((m) => m.status !== 'streaming')

  const scrollToBottom = useCallback((behavior: ScrollBehavior = 'smooth') => {
    const el = scrollRef.current
    if (!el) return
    el.scrollTo({ top: el.scrollHeight, behavior })
    parked.current = true
    setShowJump(false)
  }, [])

  const handleScroll = useCallback(() => {
    const el = scrollRef.current
    if (!el) return
    const distance = el.scrollHeight - el.scrollTop - el.clientHeight
    parked.current = distance <= BOTTOM_SLACK
    setShowJump(!parked.current)
  }, [])

  // Follow the tail while the reader has not scrolled away from it.
  useLayoutEffect(() => {
    if (parked.current) scrollToBottom('auto')
  }, [persisted.length, stream?.text, scrollToBottom])

  const handleSend = useCallback(async () => {
    const content = input.trim()
    if (!content) return
    setInput('')

    if (conversationId) {
      await sendMessage(content)
      return
    }

    // No conversation yet: create one, hand the prompt to the next route, and
    // let it fire the request once it owns the id.
    setPendingPrompt(content)
    const created = await createConversation.mutateAsync({})
    await navigate({ to: '/chat/$conversationId', params: { conversationId: created.id } })
  }, [conversationId, createConversation, input, navigate, sendMessage, setPendingPrompt])

  // Consume a prompt carried over from the empty state.
  useEffect(() => {
    if (!conversationId) return
    const pending = takePendingPrompt()
    if (pending) void sendMessage(pending)
  }, [conversationId, sendMessage, takePendingPrompt])

  const title = conversationId ? (conversation.data?.title ?? '') : t('next.nav.new')
  const isEmpty = !conversationId || (!messages.isPending && persisted.length === 0 && !working)

  return (
    <main className="n-main">
      <div className="n-topbar">
        <SidebarToggle />
        <div className="truncate font-medium">{title}</div>
      </div>

      <div className="n-scroll" ref={scrollRef} onScroll={handleScroll}>
        {conversationId && messages.isPending ? (
          <div className="n-loading" role="status" aria-label={t('state.loading')}>
            <span className="n-spinner" />
          </div>
        ) : isEmpty ? (
          <Welcome onPick={setInput} modelName={MODEL_NAME} />
        ) : (
          <div className="n-column">
            {persisted.map((message) => (
              <MessageItem
                key={message.id}
                role={message.role}
                content={message.content}
                model={MODEL_NAME}
              />
            ))}

            {working && (
              <MessageItem
                role="assistant"
                content={stream?.text ?? ''}
                model={MODEL_NAME}
                streaming
              />
            )}
          </div>
        )}
      </div>

      {stream?.error && (
        <div className="n-column px-4 text-sm" style={{ color: 'var(--n-err)' }} role="alert">
          {stream.error}
        </div>
      )}

      <div className="n-composer-dock">
        <div className="n-column relative">
          {showJump && (
            <button
              type="button"
              className="n-scroll-bottom"
              aria-label={t('next.scroll-bottom')}
              onClick={() => scrollToBottom()}
            >
              <ArrowDown className="h-4 w-4" />
            </button>
          )}
          <Composer
            value={input}
            onValueChange={setInput}
            onSend={() => void handleSend()}
            onStop={stop}
            working={working}
            disabled={createConversation.isPending}
          />
        </div>
      </div>
    </main>
  )
}

export default NextChat
