import type { Conversation } from '@openchat/api-client'
import { Outlet, useLocation, useNavigate, useParams } from '@tanstack/react-router'
import { PanelLeft } from 'lucide-react'
import { AnimatePresence, motion, useReducedMotion } from 'motion/react'
import { createContext, useCallback, useContext, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'

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
import { useDeleteConversation } from '@/hooks/use-conversations'
import Sidebar from './Sidebar'

interface NextShellContextValue {
  collapsed: boolean
  toggleCollapsed: () => void
}

const routeVariants = {
  hidden: { opacity: 0, y: 10 },
  visible: { opacity: 1, y: 0, transition: { duration: 0.28, ease: [0.22, 1, 0.36, 1] as const } },
  exit: { opacity: 0, y: -6, transition: { duration: 0.16, ease: [0.4, 0, 1, 1] as const } },
}

const reducedRouteVariants = {
  hidden: { opacity: 1, y: 0 },
  visible: { opacity: 1, y: 0, transition: { duration: 0 } },
  exit: { opacity: 1, y: 0, transition: { duration: 0 } },
}

const NextShellContext = createContext<NextShellContextValue | null>(null)

export function SidebarToggle() {
  const context = useContext(NextShellContext)
  const { t } = useTranslation()

  if (!context) throw new Error('SidebarToggle must be rendered inside NextShell')

  return (
    <button
      type="button"
      className="n-icon-btn"
      aria-label={t('nav.toggleSidebar')}
      onClick={context.toggleCollapsed}
    >
      <PanelLeft className="h-[18px] w-[18px]" />
    </button>
  )
}

export function NextShell() {
  const { t } = useTranslation()
  const location = useLocation()
  const navigate = useNavigate()
  const reduceMotion = useReducedMotion()
  const deleteConversation = useDeleteConversation()

  const [collapsed, setCollapsed] = useState(false)
  const [pendingDelete, setPendingDelete] = useState<Conversation | null>(null)

  const toggleCollapsed = useCallback(() => setCollapsed((value) => !value), [])
  const shellContext = useMemo(
    () => ({ collapsed, toggleCollapsed }),
    [collapsed, toggleCollapsed],
  )

  const params = useParams({ strict: false })
  const activeId = 'conversationId' in params ? params.conversationId : undefined

  const handleDelete = useCallback(async () => {
    if (!pendingDelete) return
    const removed = pendingDelete.id
    setPendingDelete(null)
    await deleteConversation.mutateAsync(removed)
    if (removed === activeId) await navigate({ to: '/' })
  }, [activeId, deleteConversation, navigate, pendingDelete])

  return (
    <NextShellContext.Provider value={shellContext}>
      <div className="prism-next">
        <Sidebar collapsed={collapsed} activeId={activeId} onDelete={setPendingDelete} />

        <AnimatePresence mode="wait" initial={!reduceMotion}>
          <motion.div
            className="n-route-motion"
            key={location.pathname}
            variants={reduceMotion ? reducedRouteVariants : routeVariants}
            initial="hidden"
            animate="visible"
            exit="exit"
          >
            <Outlet />
          </motion.div>
        </AnimatePresence>

        <AlertDialog
          open={pendingDelete !== null}
          onOpenChange={(open) => !open && setPendingDelete(null)}
        >
          <AlertDialogContent>
            <AlertDialogHeader>
              <AlertDialogTitle>{t('next.conversation.remove-title')}</AlertDialogTitle>
              <AlertDialogDescription>
                {t('next.conversation.remove-description')}
                <strong> {pendingDelete?.title}</strong>
              </AlertDialogDescription>
            </AlertDialogHeader>
            <AlertDialogFooter>
              <AlertDialogCancel>{t('next.conversation.cancel')}</AlertDialogCancel>
              <AlertDialogAction onClick={() => void handleDelete()}>
                {t('chat.delete')}
              </AlertDialogAction>
            </AlertDialogFooter>
          </AlertDialogContent>
        </AlertDialog>
      </div>
    </NextShellContext.Provider>
  )
}

export default NextShell
