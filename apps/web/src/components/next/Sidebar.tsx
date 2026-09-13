import type { Conversation } from '@openchat/api-client'
import { useNavigate } from '@tanstack/react-router'
import {
  Check,
  Languages,
  LogOut,
  Monitor,
  Moon,
  MoreHorizontal,
  PencilLine,
  SquarePen,
  Sun,
  Trash2,
  UserRound,
} from 'lucide-react'
import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'

import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { useLogout, useSession } from '@/features/auth/use-auth'
import { useConversations, useRenameConversation } from '@/hooks/use-conversations'
import { groupConversations, formatRelative } from '@/lib/grouping'
import { supportedLanguages } from '@/lib/i18n'
import { cn } from '@/lib/utils'
import { useUIStore, type Theme } from '@/stores/ui-store'
import ModelAvatar from './ModelAvatar'

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
        // A menu's keyboard events bubble through the row, including its portal.
        if (event.target !== event.currentTarget) return
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
            <PencilLine className="h-4 w-4" />
            <span>{t('chat.rename')}</span>
          </DropdownMenuItem>
          <DropdownMenuItem onClick={() => onDelete(conversation)}>
            <Trash2 className="h-4 w-4" />
            <span>{t('chat.delete')}</span>
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
  const { data: session } = useSession()
  const logout = useLogout()
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

        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <button type="button" className="n-row">
              <ThemeIcon className="h-4 w-4 shrink-0" />
              <span className="n-row-label">{t('nav.toggleTheme')}</span>
            </button>
          </DropdownMenuTrigger>
          <DropdownMenuContent className="match-trigger" align="start" side="bottom">
            {THEMES.map(({ value, icon: Icon }) => (
              <DropdownMenuItem key={value} onSelect={() => setTheme(value)}>
                <Icon className="h-4 w-4" />
                <span>{t(`next.theme.${value}`)}</span>
                {theme === value && <Check className="n-menu-check h-3.5 w-3.5" />}
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
          <DropdownMenuContent className="match-trigger" align="start" side="bottom">
            {supportedLanguages.map((lang) => (
              <DropdownMenuItem key={lang} onSelect={() => void i18n.changeLanguage(lang)}>
                <span>{LANGUAGE_LABELS[lang] ?? lang}</span>
                {i18n.resolvedLanguage === lang && <Check className="n-menu-check h-3.5 w-3.5" />}
              </DropdownMenuItem>
            ))}
          </DropdownMenuContent>
        </DropdownMenu>
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

      <div className="n-sidebar-account">
        {session && (
          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <button type="button" className="n-row n-account-trigger">
                <ModelAvatar name={session.name || session.email} size={22} />
                <span className="n-row-label">{session.name || session.email}</span>
              </button>
            </DropdownMenuTrigger>
            <DropdownMenuContent className="n-account-menu" align="end" side="top">
              <DropdownMenuLabel>
                <ModelAvatar name={session.name || session.email} size={18} />
                <span className="n-menu-label-text">{session.name || session.email}</span>
              </DropdownMenuLabel>
              <DropdownMenuSeparator />
              {session.role === 'admin' && (
                <DropdownMenuItem onSelect={() => void navigate({ to: '/admin' })}>
                  <UserRound className="h-4 w-4" />
                  <span>{t('admin.title')}</span>
                </DropdownMenuItem>
              )}
              <DropdownMenuItem
                onSelect={async () => {
                  await logout.mutateAsync()
                  await navigate({ to: '/login' })
                }}
              >
                <LogOut className="h-4 w-4" />
                <span>{t('auth.logout')}</span>
              </DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>
        )}
      </div>
    </aside>
  )
}

export default Sidebar
