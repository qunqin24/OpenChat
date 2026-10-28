import { useTranslation } from 'react-i18next'

import { formatSize } from '@/lib/attachments'
import { fileKind } from '@/lib/file-look'

/** "Word 文档 · 37 KB": what a file is, and how large, under its name (FileBadge's cards). */
export function useFileSummary() {
  const { t } = useTranslation()
  return (filename: string, contentType: string, size: number) =>
    `${t(`chat.file.kind.${fileKind(filename, contentType)}`)} · ${formatSize(size)}`
}
