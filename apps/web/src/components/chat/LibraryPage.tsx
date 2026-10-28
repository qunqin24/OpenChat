import { isOpenChatError, type LibraryFile } from '@openchat/api-client'
import { useQueryClient } from '@tanstack/react-query'
import { Link, useNavigate } from '@tanstack/react-router'
import { Download, LayoutGrid, List, MessageSquare, MoreHorizontal, Search, Trash2 } from 'lucide-react'
import { useEffect, useMemo, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@/components/ui/alert-dialog'
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from '@/components/ui/dropdown-menu'
import { useDebounced } from '@/hooks/use-debounced'
import { downloadFile, useDeleteFile, useLibraryFiles } from '@/hooks/use-library'
import { formatSize, opensInPanel } from '@/lib/attachments'
import { hasPagePreview } from '@/lib/file-preview'
import { libraryFilters, withoutFile, type LibraryFilter, type LibraryPages } from '@/lib/library'
import { queryKeys } from '@/lib/query-keys'
import { useDisplayTitle } from '@/lib/titles'
import { cn } from '@/lib/utils'
import { LIBRARY, useFilePanel } from '@/stores/file-panel-store'
import { useImageViewer, type ViewerImage } from '@/stores/image-viewer-store'
import { useUIStore } from '@/stores/ui-store'
import { SidebarToggle } from './ChatShell'
import { FileIcon } from './FileIcon'
import { FileBadge } from './FileBadge'
import { FilePagePreview } from './FilePagePreview'
import FilePanel from './FilePanel'

/**
 * The library: every file the person sent, across their conversations, newest
 * first, as a grid of thumbnails or a list. A file opens in a new tab, can be
 * saved, followed back to its conversation, or deleted for good - its message
 * then shows where it was (MessageAttachments).
 */
export default function LibraryPage() {
  const { t, i18n } = useTranslation()
  const queryClient = useQueryClient()
  const [typed, setTyped] = useState('')
  const search = useDebounced(typed.trim(), 250)
  const [filter, setFilter] = useState<LibraryFilter>('all')
  const view = useUIStore((s) => s.libraryView)
  const setView = useUIStore((s) => s.setLibraryView)
  const files = useLibraryFiles(search, filter)
  const remove = useDeleteFile()
  const [pending, setPending] = useState<LibraryFile | null>(null)
  const scroll = useRef<HTMLDivElement>(null)
  const loadMore = useRef<HTMLButtonElement>(null)

  const items = useMemo(() => files.data?.pages.flatMap((page) => page.items) ?? [], [files.data])
  const openViewer = useImageViewer((s) => s.open)
  // A file opened here is read over the whole window, and goes with the page.
  const filePanel = useFilePanel()
  const openFile = filePanel.open
  const fileOpen = filePanel.conversationId === LIBRARY && filePanel.file !== null
  useEffect(
    () => () => {
      if (useFilePanel.getState().conversationId === LIBRARY) useFilePanel.getState().close()
    },
    [],
  )
  // The images of the list as it stands, for the viewer to step through.
  const pictures = useMemo(() => items.filter((file) => file.contentType.startsWith('image/')), [items])
  const date = useMemo(
    () => new Intl.DateTimeFormat(i18n.resolvedLanguage, { year: 'numeric', month: 'short', day: 'numeric' }),
    [i18n.resolvedLanguage],
  )

  const { hasNextPage, isFetchingNextPage, isFetchNextPageError, fetchNextPage } = files
  useEffect(() => {
    const target = loadMore.current
    if (!target || !hasNextPage || isFetchingNextPage || isFetchNextPageError) return
    const observer = new IntersectionObserver(([entry]) => {
      if (entry?.isIntersecting) void fetchNextPage()
    }, { root: scroll.current, rootMargin: '0px 0px 200px 0px' })
    observer.observe(target)
    return () => observer.disconnect()
  }, [hasNextPage, isFetchingNextPage, isFetchNextPageError, fetchNextPage])

  const save = async (file: LibraryFile) => {
    try {
      await downloadFile(file.id)
    } catch {
      toast.error(t('library.download-failed'))
    }
  }

  const confirmDelete = async () => {
    if (!pending) return
    const file = pending
    setPending(null)
    try {
      await remove.mutateAsync(file)
      toast.success(t('library.deleted', { name: file.filename }))
    } catch (error) {
      // Already gone (its conversation was deleted, another tab) is what was asked for.
      if (isOpenChatError(error) && error.status === 404) {
        queryClient.setQueriesData<LibraryPages>({ queryKey: queryKeys.library.all }, (held) => withoutFile(held, file.id))
      } else {
        toast.error(t('library.delete-failed'))
      }
    }
  }

  // An image opens in the viewer, among the list's others; a text, Word or
  // Excel file in the side panel, as from a conversation; a PDF in a new tab.
  const showImage = (file: LibraryFile) => (event: React.MouseEvent) => {
    if (event.metaKey || event.ctrlKey || event.shiftKey) return
    if (opensInPanel(file.contentType)) {
      event.preventDefault()
      openFile(LIBRARY, file)
      return
    }
    if (!file.contentType.startsWith('image/')) return
    event.preventDefault()
    const gallery: ViewerImage[] = pictures.map((p) => ({ src: p.url, title: p.filename, download: () => void save(p) }))
    openViewer(gallery, pictures.indexOf(file))
  }

  const filtered = search !== '' || filter !== 'all'

  return (
    <main className="n-main n-library">
      <div className="n-topbar">
        <SidebarToggle />
      </div>

      <div className="n-scroll" ref={scroll}>
        <div className="n-library-column">
          <header className="n-library-head">
            <h1 className="n-library-title">{t('library.title')}</h1>
            <label className="n-library-search">
              <Search aria-hidden="true" />
              <input
                type="search"
                value={typed}
                maxLength={200}
                placeholder={t('library.search')}
                aria-label={t('library.search')}
                onChange={(event) => setTyped(event.target.value)}
              />
            </label>
          </header>

          <div className="n-library-bar">
            <div className="n-library-filters" role="group" aria-label={t('library.filter')}>
              {libraryFilters.map((value) => (
                <button
                  key={value}
                  type="button"
                  className={cn('n-library-filter', filter === value && 'active')}
                  aria-pressed={filter === value}
                  onClick={() => setFilter(value)}
                >
                  {t(`library.type.${value}`)}
                </button>
              ))}
            </div>
            <div className="n-library-views" role="group" aria-label={t('library.view.label')}>
              <button type="button" className={cn('n-icon-btn', view === 'grid' && 'on')} aria-pressed={view === 'grid'} aria-label={t('library.view.grid')} onClick={() => setView('grid')}>
                <LayoutGrid className="h-4 w-4" />
              </button>
              <button type="button" className={cn('n-icon-btn', view === 'list' && 'on')} aria-pressed={view === 'list'} aria-label={t('library.view.list')} onClick={() => setView('list')}>
                <List className="h-4 w-4" />
              </button>
            </div>
          </div>

          {files.isPending ? (
            <div className="n-library-state" role="status" aria-label={t('state.loading')}>
              <span className="n-spinner" data-motion="essential" />
            </div>
          ) : files.isError ? (
            <div className="n-library-state">
              <button type="button" className="n-library-retry" onClick={() => void files.refetch()}>{t('library.retry')}</button>
            </div>
          ) : items.length === 0 ? (
            <div className="n-library-state">
              <p className="n-library-empty-title">{filtered ? t('library.no-match') : t('library.empty')}</p>
              {!filtered && <p className="n-library-empty-hint">{t('library.empty-hint')}</p>}
            </div>
          ) : view === 'grid' ? (
            <ul className="n-library-grid">
              {items.map((file) => (
                <li key={file.id} className="n-library-card">
                  <a className="n-library-thumb" href={file.url} target="_blank" rel="noreferrer" aria-label={t('chat.file.open', { name: file.filename })} onClick={showImage(file)}>
                    <Preview file={file} page />
                  </a>
                  <div className="n-library-card-foot">
                    {hasPagePreview(file.contentType) && <FileBadge filename={file.filename} contentType={file.contentType} />}
                    <div className="n-library-card-meta">
                      <span className="n-library-name" dir="auto" title={file.filename}>{file.filename}</span>
                      <span className="n-library-sub">{formatSize(file.size)} · {date.format(new Date(file.createdAt))}</span>
                    </div>
                    <FileMenu file={file} onDownload={save} onDelete={setPending} />
                  </div>
                </li>
              ))}
            </ul>
          ) : (
            <div className="n-library-list" role="table" aria-label={t('library.title')}>
              <div className="n-library-row is-head" role="row">
                <span role="columnheader">{t('library.column.name')}</span>
                <span role="columnheader">{t('library.column.conversation')}</span>
                <span role="columnheader" className="n-library-size">{t('library.column.size')}</span>
                <span role="columnheader" className="n-library-date">{t('library.column.date')}</span>
                <span role="columnheader" aria-label={t('chat.more')} />
              </div>
              {items.map((file) => (
                <div key={file.id} className="n-library-row" role="row">
                  <a role="cell" className="n-library-row-name" href={file.url} target="_blank" rel="noreferrer" onClick={showImage(file)}>
                    <span className="n-library-row-thumb"><Preview file={file} /></span>
                    <span className="n-library-name" dir="auto" title={file.filename}>{file.filename}</span>
                  </a>
                  <span role="cell" className="n-library-row-source"><SourceLink file={file} /></span>
                  <span role="cell" className="n-library-size">{formatSize(file.size)}</span>
                  <span role="cell" className="n-library-date">{date.format(new Date(file.createdAt))}</span>
                  <span role="cell"><FileMenu file={file} onDownload={save} onDelete={setPending} /></span>
                </div>
              ))}
            </div>
          )}

          {hasNextPage && (
            <button ref={loadMore} type="button" className="n-library-more" disabled={isFetchingNextPage} onClick={() => void fetchNextPage()}>
              {isFetchingNextPage ? t('state.loading') : isFetchNextPageError ? t('library.retry') : t('library.load-more')}
            </button>
          )}
        </div>
      </div>

      <AlertDialog open={pending !== null} onOpenChange={(open) => !open && setPending(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t('library.delete-title')}</AlertDialogTitle>
            <AlertDialogDescription>{t('library.delete-description', { name: pending?.filename ?? '' })}</AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{t('chat.conversation.cancel')}</AlertDialogCancel>
            <AlertDialogAction onClick={() => void confirmDelete()}>{t('chat.delete')}</AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
      {fileOpen && filePanel.file && <FilePanel key={filePanel.file.id} file={filePanel.file} full />}
    </main>
  )
}

/**
 * An image's thumbnail; in the grid, a page of a file's first words
 * (FilePagePreview); else the file icon - also for an image whose link has
 * expired.
 */
function Preview({ file, page = false }: { file: LibraryFile; page?: boolean }) {
  const [broken, setBroken] = useState(false)
  if (file.contentType.startsWith('image/') && !broken) {
    return <img src={file.url} alt="" loading="lazy" decoding="async" onError={() => setBroken(true)} />
  }
  if (page && hasPagePreview(file.contentType)) return <FilePagePreview file={file} />
  return (
    <span className="n-library-icon" aria-hidden="true">
      <FileIcon filename={file.filename} contentType={file.contentType} />
    </span>
  )
}

function SourceLink({ file }: { file: LibraryFile }) {
  const display = useDisplayTitle()
  return (
    <Link className="n-library-source" to="/chat/$conversationId" params={{ conversationId: file.conversationId }} dir="auto">
      {display(file.conversationTitle)}
    </Link>
  )
}

function FileMenu({ file, onDownload, onDelete }: {
  file: LibraryFile
  onDownload: (file: LibraryFile) => void
  onDelete: (file: LibraryFile) => void
}) {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const display = useDisplayTitle()
  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <button type="button" className="n-icon-btn n-library-menu" aria-label={t('library.actions', { name: file.filename })}>
          <MoreHorizontal className="h-4 w-4" />
        </button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end">
        <DropdownMenuItem onSelect={() => void navigate({ to: '/chat/$conversationId', params: { conversationId: file.conversationId } })}>
          <MessageSquare /><span className="n-library-menu-label">{t('library.open-chat', { title: display(file.conversationTitle) })}</span>
        </DropdownMenuItem>
        <DropdownMenuItem onSelect={() => onDownload(file)}><Download /><span>{t('library.download')}</span></DropdownMenuItem>
        <DropdownMenuItem onSelect={() => onDelete(file)}><Trash2 /><span>{t('chat.delete')}</span></DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  )
}
