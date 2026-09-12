import { Link, useNavigate, useParams } from '@tanstack/react-router'
import { AnimatePresence, motion } from 'motion/react'
import { MessageSquare, PanelLeftClose, Plus, Trash2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { LanguageSwitcher } from '@/components/layout/language-switcher'
import { ThemeToggle } from '@/components/layout/theme-toggle'
import { Button } from '@/components/ui/button'
import { Tooltip } from '@/components/ui/tooltip'
import { useConversations, useCreateConversation, useDeleteConversation } from '@/hooks/use-conversations'
import { cn } from '@/lib/utils'
import { useUIStore } from '@/stores/ui-store'

export function Sidebar() {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const toggleSidebar = useUIStore((s) => s.toggleSidebar)

  const { data, isPending } = useConversations()
  const createConversation = useCreateConversation()
  const deleteConversation = useDeleteConversation()

  const params = useParams({ strict: false })
  const activeId = 'conversationId' in params ? params.conversationId : undefined

  const handleCreate = async () => {
    const conversation = await createConversation.mutateAsync({})
    await navigate({ to: '/chat/$conversationId', params: { conversationId: conversation.id } })
  }

  const handleDelete = async (id: string) => {
    await deleteConversation.mutateAsync(id)
    if (id === activeId) await navigate({ to: '/' })
  }

  return (
    <aside className="flex h-full w-72 shrink-0 flex-col border-r border-border bg-surface-muted">
      <header className="flex items-center gap-2 px-3 py-3">
        <span className="flex-1 truncate text-sm font-semibold">{t('app.name')}</span>
        <Tooltip label={t('nav.toggleSidebar')}>
          <Button
            variant="ghost"
            size="icon"
            onClick={toggleSidebar}
            aria-label={t('nav.toggleSidebar')}
          >
            <PanelLeftClose />
          </Button>
        </Tooltip>
      </header>

      <div className="px-3 pb-3">
        <Button
          variant="primary"
          className="w-full"
          onClick={() => void handleCreate()}
          disabled={createConversation.isPending}
        >
          <Plus />
          {t('nav.newChat')}
        </Button>
      </div>

      <nav
        aria-label={t('nav.conversations')}
        className="scrollbar-thin flex-1 overflow-y-auto px-2 pb-2"
      >
        {isPending && (
          <p className="px-2 py-4 text-sm text-muted-foreground">{t('state.loading')}</p>
        )}

        {!isPending && data?.items.length === 0 && (
          <p className="px-2 py-4 text-sm text-muted-foreground">{t('state.empty')}</p>
        )}

        <AnimatePresence initial={false}>
          {data?.items.map((conversation) => (
            <motion.div
              key={conversation.id}
              layout
              initial={{ opacity: 0, y: -4 }}
              animate={{ opacity: 1, y: 0 }}
              exit={{ opacity: 0, height: 0 }}
              transition={{ duration: 0.15 }}
              className="group relative"
            >
              <Link
                to="/chat/$conversationId"
                params={{ conversationId: conversation.id }}
                className={cn(
                  'flex items-center gap-2 rounded-card px-2 py-2 pr-9 text-sm transition-colors',
                  conversation.id === activeId
                    ? 'bg-surface-hover text-foreground'
                    : 'text-muted-foreground hover:bg-surface-hover hover:text-foreground',
                )}
              >
                <MessageSquare className="size-4 shrink-0" />
                <span className="truncate">{conversation.title || t('chat.untitled')}</span>
              </Link>

              <Button
                variant="ghost"
                size="icon"
                aria-label={t('chat.delete')}
                onClick={() => void handleDelete(conversation.id)}
                className="absolute top-1/2 right-1 size-7 -translate-y-1/2 opacity-0 transition-opacity group-hover:opacity-100 focus-visible:opacity-100"
              >
                <Trash2 className="size-3.5" />
              </Button>
            </motion.div>
          ))}
        </AnimatePresence>
      </nav>

      <footer className="flex items-center gap-1 border-t border-border px-3 py-2">
        <ThemeToggle />
        <LanguageSwitcher />
      </footer>
    </aside>
  )
}
