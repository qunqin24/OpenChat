import { createFileRoute } from '@tanstack/react-router'
import { useTranslation } from 'react-i18next'

import { Composer } from '@/components/chat/composer'
import { MessageList } from '@/components/chat/message-list'
import { useConversation } from '@/hooks/use-conversations'
import { useMessages } from '@/hooks/use-messages'
import { useSendMessage } from '@/hooks/use-send-message'
import { useChatStore } from '@/stores/chat-store'

export const Route = createFileRoute('/chat/$conversationId')({
  component: ChatRoute,
})

function ChatRoute() {
  const { conversationId } = Route.useParams()
  const { t } = useTranslation()

  const conversation = useConversation(conversationId)
  const messages = useMessages(conversationId)
  const sendMessage = useSendMessage(conversationId)

  // The server persists the assistant row before the first token, so hide rows
  // that are still streaming; the live bubble below renders them instead.
  const persisted = (messages.data?.items ?? []).filter((m) => m.status !== 'streaming')

  const stream = useChatStore((s) => s.stream)
  const stop = useChatStore((s) => s.stop)
  const isStreaming = stream !== null && stream.conversationId === conversationId

  return (
    <div className="flex h-full flex-col">
      <header className="flex h-14 shrink-0 items-center border-b border-border px-4 pl-14">
        <h1 className="truncate text-sm font-medium">
          {conversation.data?.title ?? t('chat.untitled')}
        </h1>
      </header>

      {messages.isPending ? (
        <p className="flex-1 px-4 py-6 text-sm text-muted-foreground">{t('state.loading')}</p>
      ) : (
        <MessageList
          messages={persisted}
          streamingText={stream?.text}
          isStreaming={isStreaming}
        />
      )}

      {stream?.error && (
        <p role="alert" className="mx-auto w-full max-w-3xl px-4 text-sm text-destructive">
          {stream.error}
        </p>
      )}

      <Composer
        onSend={(content) => void sendMessage(content)}
        onStop={stop}
        isStreaming={isStreaming}
        disabled={conversation.isError}
      />
    </div>
  )
}
