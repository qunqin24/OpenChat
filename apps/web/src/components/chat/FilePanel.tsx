import { useQuery } from '@tanstack/react-query'
import { Check, Copy, Download, X } from 'lucide-react'
import { lazy, Suspense, useEffect, useMemo, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { downloadFile } from '@/hooks/use-library'
import { API_BASE_URL } from '@/lib/api'
import { DOCX_TYPE, formatSize, isDocument, PDF_TYPE, TEXT_TYPE, XLSX_TYPE } from '@openchat/chat-core/attachments'
import { copyClipboard } from '@/lib/clipboard'
import { fileLook, isMarkdownFile, looksLikeCode } from '@openchat/chat-core/file-look'
import { cn } from '@/lib/utils'
import { delimiterOf, parseDelimited, parseWorkbook } from '@openchat/chat-core/workbook'
import { useFilePanel, type OpenFile } from '@/stores/file-panel-store'
import { FileIcon } from './FileIcon'
import { Markdown } from './MarkdownText'
import { SheetView } from './file-views/SheetView'

// The two that bring a library of their own, loaded when such a file is opened.
const WordView = lazy(() => import('./file-views/WordView'))
const PdfView = lazy(() => import('./file-views/PdfView'))

/**
 * A file someone sent, read beside the chat: its name, copy, download and
 * close above the file. A text file is shown as it is, read from the file's
 * own signed link (the bucket's CORS allows GET, docs/deployment.md), a
 * Markdown one drawn as the chat draws an answer; a Word
 * file laid out as Word lays it out (WordView) and a PDF page by page
 * (PdfView), both from that link too; an Excel file as a grid of its sheets
 * (SheetView) from the words the server reads out of it. Copy copies the
 * words - for a document, what a model is sent; download is the file itself.
 * A link that has expired says so rather than showing nothing. Beside a
 * conversation it is a side panel; opened from the library, `full`, it takes
 * the whole window, as a file opened on a desktop does.
 */
export function FilePanel({ file, full = false }: { file: OpenFile; full?: boolean }) {
  const { t } = useTranslation()
  const close = useFilePanel((s) => s.close)
  const text = useQuery({
    // Not in query-keys: a file's text is read for the panel and never kept
    // (query-persist keeps only what it names).
    queryKey: ['file-text', file.id],
    queryFn: async ({ signal }) => {
      const response = isDocument(file.contentType) || file.contentType === PDF_TYPE
        ? await fetch(`${API_BASE_URL}/files/${encodeURIComponent(file.id)}/text`, { signal, credentials: 'include' })
        : await fetch(file.url, { signal })
      if (!response.ok) throw new Error(`file ${response.status}`)
      return response.text()
    },
    staleTime: Infinity,
    gcTime: 60_000,
    retry: false,
  })
  // A table - an Excel file, a CSV or TSV one - is shown as a grid.
  const separator = file.contentType === TEXT_TYPE ? delimiterOf(file.filename) : null
  const sheets = useMemo(() => {
    if (text.data === undefined) return null
    if (file.contentType === XLSX_TYPE) return parseWorkbook(text.data)
    return separator ? [parseDelimited(text.data, file.filename, separator)] : null
  }, [text.data, file.contentType, file.filename, separator])
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
    <aside className={cn('n-file-panel', full && 'is-full')} aria-label={file.filename}>
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
        {file.contentType === DOCX_TYPE || file.contentType === PDF_TYPE ? (
          <Suspense fallback={<p className="n-file-panel-note">{t('chat.file.loading')}</p>}>
            {file.contentType === DOCX_TYPE ? <WordView url={file.url} /> : <PdfView url={file.url} />}
          </Suspense>
        ) : text.isError ? (
          <p className="n-file-panel-note">{t('chat.file.load-failed')}</p>
        ) : text.data === undefined ? (
          <p className="n-file-panel-note">{t('chat.file.loading')}</p>
        ) : sheets ? (
          <SheetView sheets={sheets} />
        ) : file.contentType === TEXT_TYPE && isMarkdownFile(file.filename) ? (
          <div className="n-file-panel-markdown" dir="auto">
            <Markdown text={text.data} />
          </div>
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
