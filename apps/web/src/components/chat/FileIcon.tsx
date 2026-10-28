import { Braces, CodeXml, File, FileText, Globe, Image, NotepadText, Sheet } from 'lucide-react'

import { fileLook, type FileLook } from '@/lib/file-look'
import { cn } from '@/lib/utils'

const ICONS: Record<FileLook, typeof File> = {
  note: NotepadText,
  doc: FileText,
  data: Sheet,
  config: Braces,
  web: Globe,
  code: CodeXml,
  pdf: FileText,
  image: Image,
  file: File,
}

/** A file's icon, in the colour of its kind (attachments.css). */
export function FileIcon({ filename, contentType, className }: { filename: string; contentType: string; className?: string }) {
  const look = fileLook(filename, contentType)
  const Icon = ICONS[look]
  return <Icon className={cn('n-file-icon', `is-${look}`, className)} aria-hidden="true" />
}

export default FileIcon
