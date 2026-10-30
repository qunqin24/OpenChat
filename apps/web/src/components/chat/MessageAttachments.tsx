import type { Attachment, ConversationReference, RemovedAttachment } from '@openchat/api-client'
import { FileX2 } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { toast } from 'sonner'

import { useFileSummary } from '@/hooks/use-file-summary'
import { downloadFile } from '@/hooks/use-library'
import { opensInPanel } from '@openchat/chat-core/attachments'
import { useFilePanel } from '@/stores/file-panel-store'
import { useImageViewer, type ViewerImage } from '@/stores/image-viewer-store'
import { ReferenceChip } from './ConversationChip'
import { FileBadge } from './FileBadge'

/**
 * The files a user sent with a message: images as thumbnails that open in the
 * image viewer, stepping through the message's images, a text file as a
 * chip that opens it in the side panel (FilePanel), anything else as a chip
 * that opens it in a new tab. The links are
 * signed and expire, so an image that no longer loads falls back to the chip.
 * Files deleted since (from the library) keep their place as a muted chip.
 * Then the other conversations it refers to, each a way to that conversation.
 */
export function MessageAttachments({
  conversationId,
  items,
  removed = [],
  references = [],
}: {
  conversationId: string
  items: Attachment[]
  removed?: RemovedAttachment[]
  references?: ConversationReference[]
}) {
  const { t } = useTranslation()
  const pictures = items.filter((item) => item.contentType.startsWith('image/'))
  const gallery: ViewerImage[] = pictures.map((item) => ({
    src: item.url,
    title: item.filename,
    download: () => void downloadFile(item.id).catch(() => toast.error(t('library.download-failed'))),
  }))
  return (
    <div className="n-msg-files">
      {items.map((item) =>
        item.contentType.startsWith('image/') ? (
          <ImageFile key={item.id} item={item} gallery={gallery} index={pictures.indexOf(item)} />
        ) : (
          <FileChip key={item.id} item={item} conversationId={conversationId} />
        ),
      )}
      {removed.map((item) => <RemovedChip key={item.id} item={item} />)}
      {references.map((reference) => <ReferenceChip key={reference.conversationId} reference={reference} />)}
    </div>
  )
}

function RemovedChip({ item }: { item: RemovedAttachment }) {
  const { t } = useTranslation()
  return (
    <span className="n-msg-chip is-removed" title={t('chat.file.removed-hint')}>
      <span className="n-attach-icon" aria-hidden>
        <FileX2 className="h-4 w-4" />
      </span>
      <span className="n-attach-meta">
        <span className="n-attach-name">{item.filename}</span>
        <span className="n-attach-status">{t('chat.file.removed')}</span>
      </span>
    </span>
  )
}

function ImageFile({ item, gallery, index }: { item: Attachment; gallery: ViewerImage[]; index: number }) {
  const { t } = useTranslation()
  const open = useImageViewer((s) => s.open)
  const [broken, setBroken] = useState(false)
  if (broken) return <FileChip item={item} conversationId={null} />

  return (
    <button type="button" className="n-msg-image" aria-label={t('chat.file.open', { name: item.filename })} onClick={() => open(gallery, index)}>
      <img src={item.url} alt={item.filename} loading="lazy" onError={() => setBroken(true)} />
    </button>
  )
}

function FileChip({ item, conversationId }: { item: Attachment; conversationId: string | null }) {
  const { t } = useTranslation()
  const summary = useFileSummary()
  const openFile = useFilePanel((s) => s.open)
  const body = (
    <>
      <FileBadge filename={item.filename} contentType={item.contentType} />
      <span className="n-attach-meta">
        <span className="n-attach-name">{item.filename}</span>
        <span className="n-attach-status">{summary(item.filename, item.contentType, item.size)}</span>
      </span>
    </>
  )
  if (conversationId && opensInPanel(item.contentType)) {
    return (
      <button type="button" className="n-msg-chip" aria-label={t('chat.file.open', { name: item.filename })} onClick={() => openFile(conversationId, item)}>
        {body}
      </button>
    )
  }
  return (
    <a className="n-msg-chip" href={item.url} target="_blank" rel="noreferrer" aria-label={t('chat.file.open', { name: item.filename })}>
      {body}
    </a>
  )
}

export default MessageAttachments
