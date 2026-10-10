import type { Conversation } from '@openchat/api-client'
import { Link } from '@tanstack/react-router'
import { Check, MoreHorizontal, PencilLine, Sparkles, Trash2, X } from 'lucide-react'
import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'

import {
  DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { toast } from 'sonner'

import { useGenerateTitle, useRenameConversation } from '@/hooks/use-conversations'
import { formatRelative } from '@/lib/grouping'
import { cn } from '@/lib/utils'

interface Props {
  conversation: Conversation
  active: boolean
  onDelete: (conversation: Conversation) => void
  onNavigate: () => void
}

export function SidebarConversation({ conversation, active, onDelete, onNavigate }: Props) {
  const { t } = useTranslation()
  const rename = useRenameConversation()
  const smartTitle = useGenerateTitle()
  const [menuOpen, setMenuOpen] = useState(false)
  const [editing, setEditing] = useState(false)
  const [title, setTitle] = useState(conversation.title)
  const input = useRef<HTMLInputElement>(null)
  const link = useRef<HTMLAnchorElement>(null)

  useEffect(() => {
    if (editing) { input.current?.focus(); input.current?.select() }
  }, [editing])

  const startEditing = () => {
    setTitle(conversation.title)
    rename.reset()
    setEditing(true)
  }
  const cancelEditing = () => {
    setEditing(false)
    requestAnimationFrame(() => link.current?.focus())
  }

  return (
    <div className={cn('n-conversation-row', active && 'active', menuOpen && 'menu-open', editing && 'editing')}>
      {editing ? (
        <form className="n-row n-conversation-edit" onSubmit={async (event) => {
          event.preventDefault()
          if (!title.trim() || rename.isPending) return
          if (title.trim() === conversation.title) { cancelEditing(); return }
          try {
            await rename.mutateAsync({ conversationId: conversation.id, title: title.trim() })
            cancelEditing()
          } catch { input.current?.focus() }
        }}>
          <input
            ref={input}
            value={title}
            maxLength={200}
            aria-label={t('chat.rename')}
            aria-invalid={rename.isError || undefined}
            disabled={rename.isPending}
            onChange={(event) => setTitle(event.target.value)}
            onKeyDown={(event) => {
              if (event.nativeEvent.isComposing) { if (event.key === 'Enter') event.preventDefault(); return }
              if (event.key === 'Escape' && !rename.isPending) { event.preventDefault(); event.stopPropagation(); cancelEditing() }
            }}
          />
          <button type="submit" className="n-sidebar-inline-button" disabled={!title.trim() || rename.isPending} aria-label={t('next.sidebar.save')}><Check /></button>
          <button type="button" className="n-sidebar-inline-button" disabled={rename.isPending} onClick={cancelEditing} aria-label={t('next.conversation.cancel')}><X /></button>
        </form>
      ) : (
        <>
          <Link
            ref={link}
            className={cn('n-row n-conversation-link', active && 'active')}
            to="/chat/$conversationId"
            params={{ conversationId: conversation.id }}
            aria-current={active ? 'page' : undefined}
            draggable={false}
            onClick={(event) => { if (!event.metaKey && !event.ctrlKey && !event.shiftKey && event.button === 0) onNavigate() }}
            onDoubleClick={(event) => { event.preventDefault(); startEditing() }}
          >
            {smartTitle.isPending ? (
              <span className="n-row-label n-reasoning-live" data-motion="essential">{t('chat.smart-title-pending')}</span>
            ) : (
              <span className="n-row-label" dir="auto">{conversation.title || t('chat.untitled')}</span>
            )}
            <span className="n-row-meta">{formatRelative(conversation.updatedAt, t)}</span>
          </Link>
          <DropdownMenu open={menuOpen} onOpenChange={setMenuOpen}>
            <DropdownMenuTrigger asChild>
              <button type="button" className="n-row-menu n-sidebar-inline-button" aria-label={t('next.more')}><MoreHorizontal /></button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="start" onCloseAutoFocus={(event) => { if (editing) event.preventDefault() }}>
              <DropdownMenuItem onSelect={startEditing}><PencilLine /><span>{t('chat.rename')}</span></DropdownMenuItem>
              <DropdownMenuItem
                disabled={smartTitle.isPending}
                onSelect={() =>
                  smartTitle.mutate(conversation.id, { onError: () => toast.error(t('chat.smart-title-failed')) })
                }
              >
                <Sparkles /><span>{t('chat.smart-title')}</span>
              </DropdownMenuItem>
              <DropdownMenuItem onSelect={() => onDelete(conversation)}><Trash2 /><span>{t('chat.delete')}</span></DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>
        </>
      )}
      {editing && rename.isError && <div className="n-sidebar-error" role="alert">{t('state.error')}</div>}
    </div>
  )
}
