import type { Role } from '@openchat/api-client'
import { motion } from 'motion/react'
import { Streamdown } from 'streamdown'

import { cn } from '@/lib/utils'

interface MessageBubbleProps {
  role: Role
  content: string
  /** True while tokens are still arriving, so Streamdown can close open fences. */
  streaming?: boolean
}

export function MessageBubble({ role, content, streaming = false }: MessageBubbleProps) {
  const isUser = role === 'user'

  return (
    <motion.div
      initial={{ opacity: 0, y: 8 }}
      animate={{ opacity: 1, y: 0 }}
      transition={{ duration: 0.2, ease: 'easeOut' }}
      className={cn('flex w-full', isUser ? 'justify-end' : 'justify-start')}
    >
      <div
        className={cn(
          'max-w-[min(46rem,85%)] rounded-card px-4 py-3 text-sm leading-relaxed',
          isUser ? 'bg-bubble-user' : 'bg-bubble-assistant',
        )}
      >
        {isUser ? (
          <p className="whitespace-pre-wrap break-words">{content}</p>
        ) : (
          <Streamdown parseIncompleteMarkdown={streaming} className="space-y-3">
            {content}
          </Streamdown>
        )}
      </div>
    </motion.div>
  )
}
