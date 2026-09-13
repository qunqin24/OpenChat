import type { Conversation } from '@openchat/api-client'
import { useNavigate } from '@tanstack/react-router'
import { Command } from 'cmdk'
import { Search, X } from 'lucide-react'
import { Dialog } from 'radix-ui'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { formatRelative } from '@/lib/grouping'

interface Props {
  open: boolean
  onOpenChange: (open: boolean) => void
  conversations: Conversation[]
  hasMore: boolean
  loading: boolean
  onLoadMore: () => void
  onNavigate: () => void
  onRestoreFocus: () => void
}

export function SidebarSearch({ open, onOpenChange, conversations, hasMore, loading, onLoadMore, onNavigate, onRestoreFocus }: Props) {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const [query, setQuery] = useState('')

  const close = () => { setQuery(''); onOpenChange(false) }

  return (
    <Dialog.Root open={open} onOpenChange={(value) => { if (!value) setQuery(''); onOpenChange(value) }}>
      <Dialog.Portal>
        <Dialog.Overlay className="n-sidebar-search-overlay" />
        <Dialog.Content className="n-sidebar-search-dialog" onCloseAutoFocus={(event) => { event.preventDefault(); onRestoreFocus() }}>
          <Dialog.Title className="sr-only">{t('next.sidebar.search')}</Dialog.Title>
          <Dialog.Description className="sr-only">{t('next.sidebar.search-description')}</Dialog.Description>
          <Command label={t('next.sidebar.search')} loop>
            <div className="n-sidebar-search-field">
              <Search aria-hidden="true" />
              <Command.Input value={query} onValueChange={setQuery} placeholder={t('next.sidebar.search-placeholder')} />
              <Dialog.Close className="n-sidebar-inline-button" aria-label={t('next.conversation.cancel')}><X /></Dialog.Close>
            </div>
            <Command.List className="n-sidebar-search-results">
              <Command.Empty className="n-sidebar-empty">{t('next.sidebar.search-empty')}</Command.Empty>
              {conversations.map((conversation) => (
                <Command.Item
                  key={conversation.id}
                  value={conversation.id}
                  keywords={[conversation.title]}
                  className="n-sidebar-search-result"
                  onSelect={() => {
                    close()
                    onNavigate()
                    void navigate({ to: '/chat/$conversationId', params: { conversationId: conversation.id } })
                  }}
                >
                  <span dir="auto">{conversation.title || t('chat.untitled')}</span>
                  <span>{formatRelative(conversation.updatedAt, t)}</span>
                </Command.Item>
              ))}
            </Command.List>
          </Command>
          {hasMore && (
            <button type="button" className="n-sidebar-search-more" disabled={loading} onClick={onLoadMore}>
              {loading ? t('state.loading') : t('next.sidebar.load-more')}
            </button>
          )}
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog.Root>
  )
}
