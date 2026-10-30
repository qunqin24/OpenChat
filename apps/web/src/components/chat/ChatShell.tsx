import { isOpenChatError, type Conversation } from '@openchat/api-client'
import { useQueryClient } from '@tanstack/react-query'
import { Outlet, useNavigate, useParams } from '@tanstack/react-router'
import { PanelLeft } from 'lucide-react'
import { Dialog } from 'radix-ui'
import { createContext, useCallback, useContext, useMemo, useRef, useState } from 'react'
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
import { useDeleteConversation } from '@/hooks/use-conversations'
import { queryKeys } from '@openchat/chat-core/query-keys'
import { displayTitle } from '@openchat/chat-core/titles'
import { useIsMobile } from '@/hooks/use-mobile'
import { useUIStore } from '@/stores/ui-store'
import Sidebar from './Sidebar'
import ImageViewer from './ImageViewer'
import SettingsDialog from './settings/SettingsDialog'

interface ChatShellContextValue {
  collapsed: boolean
  toggleCollapsed: () => void
  mobile: boolean
}

const ChatShellContext = createContext<ChatShellContextValue | null>(null)

export function SidebarToggle() {
  const context = useContext(ChatShellContext)
  const { t } = useTranslation()

  if (!context) throw new Error('SidebarToggle must be rendered inside ChatShell')

  if (!context.mobile) return null

  return (
    <Dialog.Trigger asChild>
      <button type="button" className="n-icon-btn" aria-label={t('chat.sidebar.open')}>
        <PanelLeft className="h-[18px] w-[18px]" />
      </button>
    </Dialog.Trigger>
  )
}

export function ChatShell() {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const queryClient = useQueryClient()
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
    try {
      await deleteConversation.mutateAsync(removed)
    } catch (error) {
      // Already gone (deleted from another tab or device) is what was asked for;
      // the list is stale, not the action failed. Anything else is said, and the
      // conversation stays where it is.
      if (isOpenChatError(error) && error.status === 404) {
        void queryClient.invalidateQueries({ queryKey: queryKeys.conversations.all })
      } else {
        toast.error(t('chat.conversation.remove-failed'))
        return
      }
    }
    if (removed === activeId) await navigate({ to: '/' })
  }, [activeId, deleteConversation, navigate, pendingDelete, queryClient, t])

  return (
    <ChatShellContext.Provider value={shellContext}>
      <Dialog.Root open={mobile && mobileOpen} onOpenChange={setMobileOpen}>
        <div className="chat-ui" ref={setHost}>
          {mobile ? (
            <Dialog.Portal>
              <Dialog.Overlay className="n-sidebar-backdrop" />
              <Dialog.Content className="chat-ui n-sidebar-mobile-frame" aria-describedby={undefined}>
                <Dialog.Title className="sr-only">{t('chat.sidebar.history')}</Dialog.Title>
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

          <div className="n-route-motion">
            <Outlet />
          </div>

          <SettingsDialog host={host} />
          <ImageViewer host={host} />

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
                <AlertDialogTitle>{t('chat.conversation.remove-title')}</AlertDialogTitle>
                <AlertDialogDescription>
                  {t('chat.conversation.remove-description', { title: displayTitle(pendingDelete?.title, t) })}
                </AlertDialogDescription>
              </AlertDialogHeader>
              <AlertDialogFooter>
                <AlertDialogCancel>{t('chat.conversation.cancel')}</AlertDialogCancel>
                <AlertDialogAction onClick={() => void handleDelete()}>
                  {t('chat.delete')}
                </AlertDialogAction>
              </AlertDialogFooter>
            </AlertDialogContent>
          </AlertDialog>
        </div>
      </Dialog.Root>
    </ChatShellContext.Provider>
  )
}

export default ChatShell
