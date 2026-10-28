import { TriangleAlert, X } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import type { PendingAttachment } from '@/hooks/use-attachments'
import { formatSize, TEXT_TYPE, type FileKind } from '@/lib/attachments'
import { cn } from '@/lib/utils'
import { FileIcon } from './FileIcon'
import Tooltip from './Tooltip'

/** A type of the kind, for the icon: the tray knows a file by its kind. */
const TYPE_OF_KIND: Record<FileKind, string> = { image: 'image/png', pdf: 'application/pdf', text: TEXT_TYPE }

/**
 * The files waiting to go with the message, above the text box: a thumbnail
 * for an image, a chip with the name for a PDF. Each shows its upload while it
 * goes up, says what went wrong if it did, and can be taken back out.
 */
export function AttachmentTray({
  items,
  onRemove,
}: {
  items: PendingAttachment[]
  onRemove: (key: string) => void
}) {
  const { t } = useTranslation()
  if (items.length === 0) return null

  return (
    <ul className="n-attach-tray" aria-label={t('chat.file.attached')}>
      {items.map((item) => (
        <AttachmentCard key={item.key} item={item} onRemove={() => onRemove(item.key)} />
      ))}
    </ul>
  )
}

function AttachmentCard({ item, onRemove }: { item: PendingAttachment; onRemove: () => void }) {
  const { t } = useTranslation()
  const percent = Math.round(item.progress * 100)
  const failure = item.error ? t(`chat.file.error.${item.error}`) : ''

  const status =
    item.status === 'uploading'
      ? t('chat.file.uploading', { percent })
      : item.status === 'error'
        ? failure
        : formatSize(item.size)

  const card = (
    <li
      className={cn('n-attach', item.kind === 'image' ? 'is-image' : `is-${item.kind}`, item.status === 'error' && 'is-error')}
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
          <span className="n-attach-icon" aria-hidden>
            {item.status === 'error' ? <TriangleAlert className="h-4 w-4" /> : <FileIcon filename={item.name} contentType={TYPE_OF_KIND[item.kind]} />}
          </span>
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
