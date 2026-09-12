import type { Conversation } from '@openchat/api-client'
import { useNavigate } from '@tanstack/react-router'
import {
  Languages,
  Monitor,
  Moon,
  MoreHorizontal,
  PencilLine,
  SquarePen,
  Sun,
  Trash2,
} from 'lucide-react'
import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'

import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { useConversations, useRenameConversation } from '@/hooks/use-conversations'
import { groupConversations, formatRelative } from '@/lib/grouping'
import { supportedLanguages } from '@/lib/i18n'
import { cn } from '@/lib/utils'
import { useUIStore, type Theme } from '@/stores/ui-store'

interface SidebarProps {
  collapsed: boolean
  /** Id of the conversation currently open, if any. */
  activeId?: string
  onDelete: (target: Conversation) => void
}

function ConversationRow({
  conversation,
  active,
  onDelete,
}: {
  conversation: Conversation
  active: boolean
  onDelete: (target: Conversation) => void
}) {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const rename = useRenameConversation()
  const [menuOpen, setMenuOpen] = useState(false)

  const open = () =>
    void navigate({ to: '/chat/$conversationId', params: { conversationId: conversation.id } })

  return (
    <div
      className={cn('n-row', active && 'active', menuOpen && 'menu-open')}
      role="button"
      tabIndex={0}
      onClick={open}
      onKeyDown={(event) => {
        if (event.key !== 'Enter' && event.key !== ' ') return
        event.preventDefault()
        open()
      }}
    >
      <div className="n-row-label">{conversation.title || t('chat.untitled')}</div>
      <div className="n-row-meta">{formatRelative(conversation.updatedAt, t)}</div>

      <DropdownMenu open={menuOpen} onOpenChange={setMenuOpen}>
        <DropdownMenuTrigger asChild>
          <button
            type="button"
            className="n-row-menu n-icon-btn sm"
            aria-label={t('next.more')}
            onClick={(event) => event.stopPropagation()}
          >
            <MoreHorizontal className="h-4 w-4" />
          </button>
        </DropdownMenuTrigger>
        <DropdownMenuContent align="end" onClick={(event) => event.stopPropagation()}>
          <DropdownMenuItem
            onClick={() => {
              const title = window.prompt(t('chat.rename'), conversation.title)
              if (title && title !== conversation.title) {
                rename.mutate({ conversationId: conversation.id, title })
              }
            }}
          >
            <PencilLine className="mr-2 h-4 w-4" />
            {t('chat.rename')}
          </DropdownMenuItem>
          <DropdownMenuItem onClick={() => onDelete(conversation)}>
            <Trash2 className="mr-2 h-4 w-4" />
            {t('chat.delete')}
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>
    </div>
  )
}

const THEMES: Array<{ value: Theme; icon: typeof Sun }> = [
  { value: 'light', icon: Sun },
  { value: 'dark', icon: Moon },
  { value: 'system', icon: Monitor },
]

const LANGUAGE_LABELS: Record<string, string> = { en: 'English', zh: '中文' }

export function Sidebar({ collapsed, activeId, onDelete }: SidebarProps) {
  const { t, i18n } = useTranslation()
  const navigate = useNavigate()

  const { data, isPending } = useConversations()
  const theme = useUIStore((s) => s.theme)
  const setTheme = useUIStore((s) => s.setTheme)

  const history = useMemo(() => data?.items ?? [], [data])
  const groups = useMemo(() => groupConversations(history, t), [history, t])

  const ThemeIcon = THEMES.find((option) => option.value === theme)?.icon ?? Monitor

  return (
    <aside className={cn('n-sidebar', collapsed && 'collapsed')}>
      <div className="px-2 pt-3">
        <div className="n-brand">
          <span className="n-brand-name">{t('app.name')}</span>
        </div>

        <button type="button" className="n-row" onClick={() => void navigate({ to: '/' })}>
          <SquarePen className="h-4 w-4 shrink-0" />
          <span className="n-row-label">{t('next.nav.new')}</span>
        </button>
      </div>

      <div className="n-sidebar-scroll">
        {groups.length === 0 ? (
          <div className="px-3 py-6 text-center text-sm" style={{ color: 'var(--n-text-3)' }}>
            {isPending ? t('state.loading') : t('next.conversation.empty')}
          </div>
        ) : (
          groups.map((group) => (
            <div key={group.key}>
              <div className="n-group">{group.label}</div>
              {group.items.map((conversation) => (
                <ConversationRow
                  key={conversation.id}
                  conversation={conversation}
                  active={conversation.id === activeId}
                  onDelete={onDelete}
                />
              ))}
            </div>
          ))
        )}
      </div>

      <div className="p-2">
        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <button type="button" className="n-row">
              <ThemeIcon className="h-4 w-4 shrink-0" />
              <span className="n-row-label">{t('nav.toggleTheme')}</span>
            </button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="start" side="top">
            {THEMES.map(({ value, icon: Icon }) => (
              <DropdownMenuItem key={value} onSelect={() => setTheme(value)}>
                <Icon className="mr-2 h-4 w-4" />
                {t(`next.theme.${value}`)}
              </DropdownMenuItem>
            ))}
          </DropdownMenuContent>
        </DropdownMenu>

        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <button type="button" className="n-row">
              <Languages className="h-4 w-4 shrink-0" />
              <span className="n-row-label">{t('nav.language')}</span>
            </button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="start" side="top">
            {supportedLanguages.map((lang) => (
              <DropdownMenuItem key={lang} onSelect={() => void i18n.changeLanguage(lang)}>
                {LANGUAGE_LABELS[lang] ?? lang}
              </DropdownMenuItem>
            ))}
          </DropdownMenuContent>
        </DropdownMenu>
      </div>
    </aside>
  )
}

export default Sidebar
