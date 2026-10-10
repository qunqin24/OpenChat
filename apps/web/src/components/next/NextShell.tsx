import type { Conversation } from '@openchat/api-client'
import { Outlet, useNavigate, useParams } from '@tanstack/react-router'
import { PanelLeft } from 'lucide-react'
import { Dialog } from 'radix-ui'
import { createContext, useCallback, useContext, useMemo, useRef, useState } from 'react'
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
import { useChatPaths } from '@/lib/chat-paths'
import { useIsMobile } from '@/hooks/use-mobile'
import { useUIStore } from '@/stores/ui-store'
import GuestNotice from './GuestNotice'
import Sidebar from './Sidebar'
import SettingsDialog from './settings/SettingsDialog'

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
  const paths = useChatPaths()
  const deleteConversation = useDeleteConversation()

  const mobile = useIsMobile()
  const sidebarOpen = useUIStore((s) => s.sidebarOpen)
  const toggleSidebar = useUIStore((s) => s.toggleSidebar)
  const [mobileOpen, setMobileOpen] = useState(false)
  const collapsed = mobile ? !mobileOpen : !sidebarOpen
  const [pendingDelete, setPendingDelete] = useState<Conversation | null>(null)
  // The shell's own scope, which the settings dialog is drawn into.
  const [host, setHost] = useState<HTMLElement | null>(null)
  // Where focus was when the confirmation was asked for. The dialog has no
  // trigger to hand focus back to, and the search palette, which can ask for
  // it, needs its field focused again for the arrow keys to work.
  const returnFocus = useRef<HTMLElement | null>(null)
  const askDelete = useCallback((conversation: Conversation) => {
    returnFocus.current = document.activeElement instanceof HTMLElement ? document.activeElement : null
    setPendingDelete(conversation)
  }, [])

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
    if (removed === activeId) await navigate({ to: paths.home })
  }, [activeId, deleteConversation, navigate, paths.home, pendingDelete])

  return (
    <NextShellContext.Provider value={shellContext}>
      <Dialog.Root open={mobile && mobileOpen} onOpenChange={setMobileOpen}>
        <div className="prism-next" ref={setHost}>
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
                  onDelete={askDelete}
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
              onDelete={askDelete}
            />
          )}

          <div className={paths.guest ? 'n-route-motion is-guest' : 'n-route-motion'}>
            {paths.guest && <GuestNotice />}
            <Outlet />
          </div>

          <SettingsDialog host={host} />

          <AlertDialog
            open={pendingDelete !== null}
            onOpenChange={(open) => !open && setPendingDelete(null)}
          >
            <AlertDialogContent
              onCloseAutoFocus={(event) => {
                const target = returnFocus.current
                returnFocus.current = null
                if (target?.isConnected) {
                  event.preventDefault()
                  target.focus()
                }
              }}
            >
              <AlertDialogHeader>
                <AlertDialogTitle>{t('next.conversation.remove-title')}</AlertDialogTitle>
                <AlertDialogDescription>
                  {t('next.conversation.remove-description', { title: pendingDelete?.title ?? '' })}
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
