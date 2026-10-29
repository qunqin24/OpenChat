import type { ConversationReference } from '@openchat/api-client'
import { Link } from '@tanstack/react-router'
import { X } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { useDisplayTitle } from '@/lib/titles'

/**
 * Another conversation a message refers to ("+" → 引用其他对话), drawn after
 * OpenWebUI's small FileItem: a pill of one line - the drawing, the title, and
 * "对话" at the far end - in the composer's tray beside the files, and on the
 * sent message. It is not a file: the server reads the conversation whenever
 * the model is asked (agent/references.go), so the pill on a sent message
 * opens the conversation itself.
 */

/**
 * Two speech bubbles, one behind the other: a back-and-forth, made for this
 * pill rather than taken from an icon set (line art on a 24 grid, as
 * StarterIcon's). The bubble behind is the lighter one (`is-accent`).
 */
export function ConversationIcon({ className }: { className?: string }) {
  return (
    <svg className={`n-drawing ${className ?? ''}`} viewBox="0 0 24 24" aria-hidden="true">
      <path className="is-accent" d="M7.5 7.5v-1a3 3 0 0 1 3-3H18a3 3 0 0 1 3 3v4a3 3 0 0 1-3 3h-1.5" />
      <path d="M6 7.5h7.5a3 3 0 0 1 3 3v3.5a3 3 0 0 1-3 3H9.6l-4.1 3.2v-3.3A3 3 0 0 1 3 14v-3.5a3 3 0 0 1 3-3Z" />
    </svg>
  )
}

function Body({ title }: { title: string }) {
  const { t } = useTranslation()
  return (
    <>
      <ConversationIcon className="n-ref-icon" />
      <span className="n-ref-title">{title}</span>
      <span className="n-ref-kind">{t('chat.reference.kind')}</span>
    </>
  )
}

/** In the composer's tray, waiting to go, with its remove. */
export function ReferenceCard({ reference, onRemove }: { reference: ConversationReference; onRemove: () => void }) {
  const { t } = useTranslation()
  const title = useDisplayTitle()(reference.title)
  return (
    <li className="n-ref" title={title}>
      <Body title={title} />
      <button type="button" className="n-ref-remove" aria-label={t('chat.file.remove', { name: title })} onClick={onRemove}>
        <X aria-hidden />
      </button>
    </li>
  )
}

/** On a sent message: a way to the conversation. */
export function ReferenceChip({ reference }: { reference: ConversationReference }) {
  const { t } = useTranslation()
  const title = useDisplayTitle()(reference.title)
  return (
    <Link
      className="n-ref is-sent"
      to="/chat/$conversationId"
      params={{ conversationId: reference.conversationId }}
      title={title}
      aria-label={t('chat.reference.open', { title })}
    >
      <Body title={title} />
    </Link>
  )
}
