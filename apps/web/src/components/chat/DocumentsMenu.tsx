import { NotebookPen } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuLabel, DropdownMenuTrigger } from '@/components/ui/dropdown-menu'
import { useConversationDocuments } from '@/hooks/use-documents'
import { formatRelative } from '@openchat/chat-core/grouping'
import { useDocumentPanel } from '@/stores/document-panel-store'

/**
 * The conversation's writing blocks, from the top bar: reopen one without
 * scrolling back to the reply that wrote it. One opens at a click; several
 * are listed, the most recently changed first. Nothing shows until the
 * conversation has one.
 */
export function DocumentsMenu({ conversationId }: { conversationId: string }) {
  const { t } = useTranslation()
  const docs = useConversationDocuments(conversationId).data ?? []
  const panel = useDocumentPanel()
  if (docs.length === 0) return null
  const open = (id: string) => panel.open(conversationId, id)
  const label = t('document.menu.label')

  const [only] = docs
  if (docs.length === 1 && only) {
    return (
      <button type="button" className="n-temp-toggle n-docs-toggle" onClick={() => open(only.id)} title={only.title}>
        <NotebookPen aria-hidden="true" />
        <span>{label}</span>
      </button>
    )
  }
  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <button type="button" className="n-temp-toggle n-docs-toggle" aria-label={t('document.menu.open', { count: docs.length })}>
          <NotebookPen aria-hidden="true" />
          <span>{label}</span>
          <span className="n-docs-count">{docs.length}</span>
        </button>
      </DropdownMenuTrigger>
      <DropdownMenuContent className="n-model-menu n-docs-menu" align="end">
        <DropdownMenuLabel>
          <span className="n-menu-label-text">{t('document.menu.heading')}</span>
        </DropdownMenuLabel>
        {docs.map((doc) => (
          <DropdownMenuItem key={doc.id} onSelect={() => open(doc.id)} className={panel.documentId === doc.id ? 'is-active' : undefined}>
            <NotebookPen aria-hidden="true" />
            <span className="n-docs-title" dir="auto">{doc.title}</span>
            <span className="n-docs-age">{formatRelative(doc.updatedAt, t)}</span>
          </DropdownMenuItem>
        ))}
      </DropdownMenuContent>
    </DropdownMenu>
  )
}

export default DocumentsMenu
