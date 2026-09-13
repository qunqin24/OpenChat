import type { Conversation } from '@openchat/api-client'
import { Outlet, useNavigate, useParams } from '@tanstack/react-router'
import { PanelLeft } from 'lucide-react'
import { Dialog } from 'radix-ui'
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
import { useIsMobile } from '@/hooks/use-mobile'
import { useUIStore } from '@/stores/ui-store'
import Sidebar from './Sidebar'

interface NextShellContextValue {
  collapsed: boolean
  toggleCollapsed: () => void
  mobile: boolean
}

const NextShellContext = createContext<NextShellContextValue | null>(null)

export function SidebarToggle() {
  const context = useContext(NextShellContext)
  const { t } = useTranslation()

  if (!context) throw new Error('SidebarToggle must be rendered inside NextShell')

  if (!context.mobile) return null

  return (
    <Dialog.Trigger asChild>
      <button type="button" className="n-icon-btn" aria-label={t('next.sidebar.open')}>
        <PanelLeft className="h-[18px] w-[18px]" />
      </button>
    </Dialog.Trigger>
  )
}

export function NextShell() {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const deleteConversation = useDeleteConversation()

  const mobile = useIsMobile()
  const sidebarOpen = useUIStore((s) => s.sidebarOpen)
  const toggleSidebar = useUIStore((s) => s.toggleSidebar)
  const [mobileOpen, setMobileOpen] = useState(false)
  const collapsed = mobile ? !mobileOpen : !sidebarOpen
  const [pendingDelete, setPendingDelete] = useState<Conversation | null>(null)

  const toggleCollapsed = useCallback(() => {
    if (mobile) setMobileOpen((value) => !value)
    else toggleSidebar()
  }, [mobile, toggleSidebar])
  const closeMobile = useCallback(() => setMobileOpen(false), [])
  const shellContext = useMemo(
    () => ({ collapsed, toggleCollapsed, mobile }),
    [collapsed, toggleCollapsed, mobile],
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
      <Dialog.Root open={mobile && mobileOpen} onOpenChange={setMobileOpen}>
        <div className="prism-next">
          {mobile ? (
            <Dialog.Portal>
              <Dialog.Overlay className="n-sidebar-backdrop" />
              <Dialog.Content className="prism-next n-sidebar-mobile-frame" aria-describedby={undefined}>
                <Dialog.Title className="sr-only">{t('next.sidebar.history')}</Dialog.Title>
                <Sidebar
                  mobile
                  collapsed={false}
                  onToggle={closeMobile}
                  onNavigate={closeMobile}
                  activeId={activeId}
                  onDelete={setPendingDelete}
                />
              </Dialog.Content>
            </Dialog.Portal>
          ) : (
            <Sidebar
              mobile={false}
              collapsed={collapsed}
              onToggle={toggleCollapsed}
              onNavigate={closeMobile}
              activeId={activeId}
              onDelete={setPendingDelete}
            />
          )}

          <div className="n-route-motion">
            <Outlet />
          </div>

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
      </Dialog.Root>
    </NextShellContext.Provider>
  )
}

export default NextShell
