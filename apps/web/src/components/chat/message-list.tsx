import type { Message } from '@openchat/api-client'
import { Loader2 } from 'lucide-react'
import { useEffect, useRef } from 'react'
import { useTranslation } from 'react-i18next'

import { MessageBubble } from '@/components/chat/message-bubble'

interface MessageListProps {
  messages: Message[]
  /** Text of the reply currently streaming in, if any. */
  streamingText?: string
  isStreaming?: boolean
}

export function MessageList({ messages, streamingText, isStreaming }: MessageListProps) {
  const { t } = useTranslation()
  const bottomRef = useRef<HTMLDivElement>(null)

  // Follow the tail of the conversation as tokens arrive.
  useEffect(() => {
    bottomRef.current?.scrollIntoView({ block: 'end' })
  }, [messages.length, streamingText])

  return (
    <div className="scrollbar-thin flex-1 overflow-y-auto">
      <div className="mx-auto flex w-full max-w-3xl flex-col gap-4 px-4 py-6">
        {messages.map((message) => (
          <MessageBubble key={message.id} role={message.role} content={message.content} />
        ))}

        {isStreaming && (
          <MessageBubble role="assistant" content={streamingText ?? ''} streaming />
        )}

        {isStreaming && !streamingText && (
          <p className="flex items-center gap-2 px-1 text-sm text-muted-foreground">
            <Loader2 className="size-4 animate-spin" />
            {t('chat.thinking')}
          </p>
        )}

        <div ref={bottomRef} />
      </div>
    </div>
  )
}
