import { badgeText, fileLook } from '@/lib/file-look'
import { cn } from '@/lib/utils'

/**
 * A file's badge: a folded-corner page in the colour of its kind, with its
 * extension on it - DOCX, XLSX, PDF, MD - where a sent file or one waiting in
 * the composer is shown (MessageAttachments, AttachmentTray).
 */
export function FileBadge({ filename, contentType }: { filename: string; contentType: string }) {
  return (
    <span className={cn('n-file-badge', `is-${fileLook(filename, contentType)}`)} aria-hidden="true">
      {badgeText(filename, contentType)}
    </span>
  )
}

export default FileBadge
