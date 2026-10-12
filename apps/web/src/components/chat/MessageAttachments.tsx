import type { Attachment } from '@openchat/api-client'
import { FileText } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { formatSize } from '@/lib/attachments'

/**
 * The files a user sent with a message: images as thumbnails that open at full
 * size in a new tab, anything else as a chip with its name. The links are
 * signed and expire, so an image that no longer loads falls back to the chip.
 */
export function MessageAttachments({ items }: { items: Attachment[] }) {
  return (
    <div className="n-msg-files">
      {items.map((item) =>
        item.contentType.startsWith('image/') ? (
          <ImageFile key={item.id} item={item} />
        ) : (
          <FileChip key={item.id} item={item} />
        ),
      )}
    </div>
  )
}

function ImageFile({ item }: { item: Attachment }) {
  const { t } = useTranslation()
  const [broken, setBroken] = useState(false)
  if (broken) return <FileChip item={item} />

  return (
    <a className="n-msg-image" href={item.url} target="_blank" rel="noreferrer" aria-label={t('chat.file.open', { name: item.filename })}>
      <img src={item.url} alt={item.filename} loading="lazy" onError={() => setBroken(true)} />
    </a>
  )
}

function FileChip({ item }: { item: Attachment }) {
  const { t } = useTranslation()
  return (
    <a className="n-msg-chip" href={item.url} target="_blank" rel="noreferrer" aria-label={t('chat.file.open', { name: item.filename })}>
      <span className="n-attach-icon" aria-hidden>
        <FileText className="h-4 w-4" />
      </span>
      <span className="n-attach-meta">
        <span className="n-attach-name">{item.filename}</span>
        <span className="n-attach-status">{formatSize(item.size)}</span>
      </span>
    </a>
  )
}

export default MessageAttachments
