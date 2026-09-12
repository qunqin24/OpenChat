import type { Role } from '@openchat/api-client'
import { Check, Copy } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Streamdown } from 'streamdown'

import { copyClipboard } from '@/lib/clipboard'
import ModelAvatar from './ModelAvatar'

interface MessageItemProps {
  role: Role
  content: string
  /** Display name for the assistant header. */
  model?: string
  /** True while tokens are still arriving. */
  streaming?: boolean
}

function CopyButton({ content }: { content: string }) {
  const { t } = useTranslation()
  const [copied, setCopied] = useState(false)

  return (
    <button
      type="button"
      className="n-icon-btn sm"
      aria-label={t('chat.copy')}
      onClick={async () => {
        if (!(await copyClipboard(content))) return
        setCopied(true)
        window.setTimeout(() => setCopied(false), 1600)
      }}
    >
      {copied ? <Check className="h-3.5 w-3.5" /> : <Copy className="h-3.5 w-3.5" />}
    </button>
  )
}

/**
 * User turns render as a right-aligned bubble with no avatar; assistant turns
 * render full width under a model-name header, matching the reference layout.
 */
export function MessageItem({ role, content, model = 'Assistant', streaming }: MessageItemProps) {
  if (role === 'user') {
    return (
      <div className="n-msg-user">
        <div className="n-bubble">{content}</div>
        <div className="n-actions">
          <CopyButton content={content} />
        </div>
      </div>
    )
  }

  const empty = content.length === 0

  return (
    <div className="n-msg-assistant">
      <ModelAvatar className="n-msg-avatar" name={model} size={32} />
      <div className="n-msg-head">{model}</div>

      {!empty && (
        <div className="n-markdown">
          {/* `markdown-body` is what the theme's 3k lines of markdown typography
              key off, so Streamdown's output is wrapped in it rather than
              restyled here. */}
          <div className="markdown-body">
            <Streamdown parseIncompleteMarkdown={streaming}>{content}</Streamdown>
          </div>
        </div>
      )}

      {streaming && empty && <span className="n-caret" />}

      {!streaming && !empty && (
        <div className="n-actions">
          <CopyButton content={content} />
        </div>
      )}
    </div>
  )
}

export default MessageItem
