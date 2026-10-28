import { useQuery } from '@tanstack/react-query'
import { Check, Copy, Download, X } from 'lucide-react'
import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { downloadFile } from '@/hooks/use-library'
import { formatSize } from '@/lib/attachments'
import { copyClipboard } from '@/lib/clipboard'
import { fileLook, looksLikeCode } from '@/lib/file-look'
import { cn } from '@/lib/utils'
import { useFilePanel, type OpenFile } from '@/stores/file-panel-store'
import { FileIcon } from './FileIcon'

/**
 * A text file someone sent, read beside the chat: its name, copy, download and
 * close above the text as it is. Read from the file's own signed link (the
 * bucket's CORS allows GET, docs/deployment.md), so nothing new is asked of
 * the server; a link that has expired says so rather than showing nothing.
 */
export function FilePanel({ file }: { file: OpenFile }) {
  const { t } = useTranslation()
  const close = useFilePanel((s) => s.close)
  const text = useQuery({
    // Not in query-keys: a file's text is read for the panel and never kept
    // (query-persist keeps only what it names).
    queryKey: ['file-text', file.id],
    queryFn: async ({ signal }) => {
      const response = await fetch(file.url, { signal })
      if (!response.ok) throw new Error(`file ${response.status}`)
      return response.text()
    },
    staleTime: Infinity,
    gcTime: 60_000,
    retry: false,
  })
  const [copied, setCopied] = useState(false)
  const timer = useRef(0)
  useEffect(() => () => window.clearTimeout(timer.current), [])
  useEffect(() => {
    const onKey = (event: KeyboardEvent) => event.key === 'Escape' && !event.defaultPrevented && close()
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [close])

  const copy = async () => {
    if (text.data === undefined || !(await copyClipboard(text.data))) return
    setCopied(true)
    window.clearTimeout(timer.current)
    timer.current = window.setTimeout(() => setCopied(false), 1500)
  }
  const download = () => void downloadFile(file.id).catch(() => toast.error(t('library.download-failed')))

  return (
    <aside className="n-file-panel" aria-label={file.filename}>
      <header className="n-doc-bar">
        <span className="n-file-panel-name" title={file.filename}>
          <FileIcon filename={file.filename} contentType={file.contentType} />
          <span dir="auto">{file.filename}</span>
        </span>
        <div className="n-doc-tools">
          <button type="button" className="n-icon-btn" aria-label={copied ? t('chat.file.copied') : t('chat.file.copy-text')} title={t('chat.file.copy-text')} disabled={text.data === undefined} onClick={() => void copy()}>
            {copied ? <Check className="h-4 w-4" /> : <Copy className="h-4 w-4" />}
          </button>
          <button type="button" className="n-icon-btn" aria-label={t('chat.file.download')} title={t('chat.file.download')} onClick={download}>
            <Download className="h-4 w-4" />
          </button>
          <button type="button" className="n-icon-btn" aria-label={t('chat.file.close-panel')} title={t('chat.file.close-panel')} onClick={close}>
            <X className="h-4 w-4" />
          </button>
        </div>
      </header>
      <div className="n-file-panel-scroll">
        {text.isError ? (
          <p className="n-file-panel-note">{t('chat.file.load-failed')}</p>
        ) : text.data === undefined ? (
          <p className="n-file-panel-note">{t('chat.file.loading')}</p>
        ) : (
          <pre className={cn('n-file-panel-text', looksLikeCode(fileLook(file.filename, file.contentType)) && 'is-code')} dir="auto">
            {text.data}
          </pre>
        )}
      </div>
      <footer className="n-doc-status">{formatSize(file.size)}</footer>
    </aside>
  )
}

export default FilePanel
