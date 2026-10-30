import type { ConversationReference } from '@openchat/api-client'
import { TriangleAlert, X } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import type { PendingAttachment } from '@/hooks/use-attachments'
import { useFileSummary } from '@/hooks/use-file-summary'
import { documentTypeOf, formatSize, TEXT_TYPE, type FileKind } from '@openchat/chat-core/attachments'
import { cn } from '@/lib/utils'
import { ReferenceCard } from './ConversationChip'
import { FileBadge } from './FileBadge'
import Tooltip from './Tooltip'

/** A type of the kind, for the icon: the tray knows a file by its kind. */
const TYPE_OF_KIND: Record<Exclude<FileKind, 'document'>, string> = { image: 'image/png', pdf: 'application/pdf', text: TEXT_TYPE }

/** A type of the file, for the icon: a document's by its name. */
const typeOf = (item: PendingAttachment) => (item.kind === 'document' ? documentTypeOf(item.name) : TYPE_OF_KIND[item.kind])

/**
 * The files waiting to go with the message, above the text box: a thumbnail
 * for an image, a chip with the name for a PDF. Each shows its upload while it
 * goes up, says what went wrong if it did, and can be taken back out. After
 * them, the other conversations the message is to refer to.
 */
export function AttachmentTray({
  items,
  onRemove,
  references = [],
  onRemoveReference,
}: {
  items: PendingAttachment[]
  onRemove: (key: string) => void
  references?: ConversationReference[]
  onRemoveReference?: (conversationId: string) => void
}) {
  const { t } = useTranslation()
  if (items.length === 0 && references.length === 0) return null

  return (
    <ul className="n-attach-tray" aria-label={t('chat.file.attached')}>
      {items.map((item) => (
        <AttachmentCard key={item.key} item={item} onRemove={() => onRemove(item.key)} />
      ))}
      {references.map((reference) => (
        <ReferenceCard
          key={reference.conversationId}
          reference={reference}
          onRemove={() => onRemoveReference?.(reference.conversationId)}
        />
      ))}
    </ul>
  )
}

function AttachmentCard({ item, onRemove }: { item: PendingAttachment; onRemove: () => void }) {
  const { t } = useTranslation()
  const summary = useFileSummary()
  const percent = Math.round(item.progress * 100)
  const failure = item.error ? t(`chat.file.error.${item.error}`) : ''

  const status =
    item.status === 'uploading'
      ? t('chat.file.uploading', { percent })
      : item.status === 'error'
        ? failure
        : item.kind === 'image'
          ? formatSize(item.size)
          : summary(item.name, typeOf(item), item.size)

  const card = (
    <li
      className={cn('n-attach', item.kind === 'image' ? 'is-image' : item.kind === 'document' ? 'is-text' : `is-${item.kind}`, item.status === 'error' && 'is-error')}
      aria-busy={item.status === 'uploading'}
    >
      {item.kind === 'image' ? (
        <>
          <img className="n-attach-img" src={item.preview} alt={item.name} />
          {item.status !== 'ready' && (
            <span className="n-attach-veil">
              {item.status === 'error' ? (
                <TriangleAlert className="h-4 w-4" aria-hidden />
              ) : (
                <span className="n-spinner sm" data-motion="essential" aria-hidden />
              )}
            </span>
          )}
          <span className="sr-only">{`${item.name}: ${status}`}</span>
        </>
      ) : (
        <>
          {item.status === 'error' ? (
            <span className="n-attach-icon" aria-hidden>
              <TriangleAlert className="h-4 w-4" />
            </span>
          ) : (
            <FileBadge filename={item.name} contentType={typeOf(item)} />
          )}
          <span className="n-attach-meta">
            <span className="n-attach-name">{item.name}</span>
            <span className="n-attach-status">{status}</span>
          </span>
        </>
      )}
      {item.status === 'uploading' && (
        <span
          className="n-attach-progress"
          role="progressbar"
          aria-label={t('chat.file.uploading', { percent })}
          aria-valuemin={0}
          aria-valuemax={100}
          aria-valuenow={percent}
        >
          <span style={{ width: `${percent}%` }} />
        </span>
      )}
      <button type="button" className="n-attach-remove" aria-label={t('chat.file.remove', { name: item.name })} onClick={onRemove}>
        <X className="h-3 w-3" aria-hidden />
      </button>
    </li>
  )

  // A thumbnail has no room for the reason, so it is a hover away.
  return item.kind === 'image' && failure ? (
    <Tooltip label={failure}>{card}</Tooltip>
  ) : (
    card
  )
}

export default AttachmentTray
