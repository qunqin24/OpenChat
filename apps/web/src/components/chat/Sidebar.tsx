import type { Conversation } from '@openchat/api-client'
import { Link, useNavigate } from '@tanstack/react-router'
import { AlarmClock, ChevronRight, LibraryBig, PanelLeft, Search, SquarePen } from 'lucide-react'
import { Collapsible } from 'radix-ui'
import { useCallback, useEffect, useMemo, useRef, useState, type CSSProperties } from 'react'
import { useTranslation } from 'react-i18next'

import { useChatFeatures } from '@/hooks/use-chat-models'
import { useConversations, usePinnedConversations, useRunningConversations } from '@/hooks/use-conversations'
import { newChatState } from '@/lib/chat-view'
import { groupConversations } from '@/lib/grouping'
import { cn } from '@/lib/utils'
import { useUIStore } from '@/stores/ui-store'
import { SidebarAccount } from './SidebarAccount'
import { SidebarConversation } from './SidebarConversation'
import { SearchPalette } from './SearchPalette'
import { SidebarResizeHandle } from './SidebarResizeHandle'
import Tooltip, { TooltipGroup } from './Tooltip'

interface SidebarProps {
  collapsed: boolean
  mobile: boolean
  onToggle: () => void
  onNavigate: () => void
  activeId?: string
  onDelete: (target: Conversation) => void
}

export function Sidebar({ collapsed, mobile, onToggle, onNavigate, activeId, onDelete }: SidebarProps) {
  const { t, i18n } = useTranslation()
  const navigate = useNavigate()
  const conversations = useConversations()
  const running = useRunningConversations()
  // The tasks page is the account's, and only while an administrator allows it.
  const features = useChatFeatures()
  const tasksOn = features.data?.scheduledTasks === true
  const width = useUIStore((s) => s.sidebarWidth)
  const setWidth = useUIStore((s) => s.setSidebarWidth)
  const chatsOpen = useUIStore((s) => s.sidebarChatsOpen)
  const setChatsOpen = useUIStore((s) => s.setSidebarChatsOpen)
  const [dragWidth, setDragWidth] = useState<number | null>(null)
  const [scrolled, setScrolled] = useState(false)
  const [searchOpen, setSearchOpen] = useState(false)
  const [searchHost, setSearchHost] = useState<HTMLElement | null>(null)
  const scroll = useRef<HTMLDivElement>(null)
  const loadMore = useRef<HTMLButtonElement>(null)
  const searchTrigger = useRef<HTMLButtonElement>(null)
  const searchOpener = useRef<HTMLElement | null>(null)
  const [modifier] = useState(() => /Mac|iPhone|iPad/.test(navigator.platform) ? '⌘' : 'Ctrl')

  const history = useMemo(() => {
    // A renamed/updated conversation can move between pages during a refetch.
    const unique = new Map<string, Conversation>()
    for (const page of conversations.data?.pages ?? []) {
      for (const item of page.items) if (!unique.has(item.id)) unique.set(item.id, item)
    }
    return [...unique.values()]
  }, [conversations.data])
  // The pinned ones stand above the rest, whatever their age, and not again
  // below among them.
  const pins = usePinnedConversations()
  const groups = useMemo(() => {
    const pinned = pins.data ?? []
    const ids = new Set(pinned.map((item) => item.id))
    const dated = groupConversations(history.filter((item) => !ids.has(item.id)), t, i18n.resolvedLanguage)
    return pinned.length > 0 ? [{ key: 'pinned', label: t('chat.group.pinned'), items: pinned }, ...dated] : dated
  }, [history, pins.data, t, i18n.resolvedLanguage])

  const openSearch = useCallback(() => {
    searchOpener.current = document.activeElement instanceof HTMLElement ? document.activeElement : null
    setSearchHost(scroll.current?.closest<HTMLElement>('.chat-ui') ?? null)
    setSearchOpen(true)
  }, [])

  useEffect(() => {
    const handleKey = (event: KeyboardEvent) => {
      if (event.defaultPrevented || event.isComposing || !(event.metaKey || event.ctrlKey)) return
      if (event.key.toLowerCase() === 'k') {
        event.preventDefault()
        openSearch()
      } else if (event.shiftKey && event.key.toLowerCase() === 'o') {
        event.preventDefault()
        setSearchOpen(false)
        onNavigate()
        void navigate({ to: '/', state: newChatState })
      }
    }
    window.addEventListener('keydown', handleKey)
    return () => window.removeEventListener('keydown', handleKey)
  }, [navigate, onNavigate, openSearch])

  const { hasNextPage, isFetchingNextPage, isFetchNextPageError, fetchNextPage } = conversations
  useEffect(() => {
    const target = loadMore.current
    if (!target || collapsed || !chatsOpen || !hasNextPage || isFetchingNextPage || isFetchNextPageError) return
    const observer = new IntersectionObserver(([entry]) => {
      if (entry?.isIntersecting) void fetchNextPage()
    }, { root: scroll.current, rootMargin: '0px 0px 100px 0px' })
    observer.observe(target)
    return () => observer.disconnect()
  }, [collapsed, chatsOpen, hasNextPage, isFetchingNextPage, isFetchNextPageError, fetchNextPage])

  const sidebarStyle = { '--n-sidebar-w': `${dragWidth ?? width}px` } as CSSProperties

  return (
    <TooltipGroup>
      <aside
        id="chat-sidebar"
        className={cn('n-sidebar', collapsed && 'collapsed', mobile && 'is-mobile', scrolled && 'has-scrolled', dragWidth !== null && 'resizing')}
        style={sidebarStyle}
        aria-label={t('chat.sidebar.history')}
      >
        {/* Open WebUI's arrangement: the rail and the panel are separate. The
            aside's own width only switches between the two, once, so the chat
            beside it lays out once per toggle; the panel then slides over it
            by clipping a box whose content keeps its full width, so nothing
            inside it rewraps while it moves. Animating the aside's width made
            the whole transcript reflow on every frame. */}
        {collapsed && !mobile && (
          <div className="n-sidebar-rail">
            <header className="n-sidebar-header">
              <Tooltip label={t('chat.sidebar.open')} side="right">
                <button type="button" className="n-sidebar-toggle" aria-label={t('chat.sidebar.open')} aria-expanded={false} aria-controls="chat-sidebar-content" onClick={onToggle}>
                  <span className="n-sidebar-rail-logo" aria-hidden="true" />
                  <PanelLeft aria-hidden="true" />
                </button>
              </Tooltip>
            </header>
            <nav className="n-sidebar-nav" aria-label={t('chat.sidebar.navigation')}>
              <Tooltip label={t('chat.nav.new')} side="right">
                <Link className="n-row n-sidebar-nav-row" to="/" state={newChatState} activeProps={{ className: '' }} aria-label={t('chat.nav.new')} aria-keyshortcuts="Control+Shift+O Meta+Shift+O">
                  <SquarePen aria-hidden="true" />
                </Link>
              </Tooltip>
              <Tooltip label={t('chat.sidebar.search')} side="right">
                <button ref={searchTrigger} type="button" className="n-row n-sidebar-nav-row" onClick={openSearch} aria-label={t('chat.sidebar.search')} aria-keyshortcuts="Control+K Meta+K">
                  <Search aria-hidden="true" />
                </button>
              </Tooltip>
              <Tooltip label={t('library.title')} side="right">
                <Link className="n-row n-sidebar-nav-row" to="/library" activeProps={{ className: 'active' }} aria-label={t('library.title')}>
                  <LibraryBig aria-hidden="true" />
                </Link>
              </Tooltip>
              {tasksOn && (
                <Tooltip label={t('tasks.title')} side="right">
                  <Link className="n-row n-sidebar-nav-row" to="/tasks" activeProps={{ className: 'active' }} aria-label={t('tasks.title')}>
                    <AlarmClock aria-hidden="true" />
                  </Link>
                </Tooltip>
              )}
            </nav>
            {/* The empty length of the rail opens the sidebar too. */}
            <button type="button" className="n-sidebar-rail-fill" tabIndex={-1} aria-hidden="true" onClick={onToggle} />
            <footer className="n-sidebar-account"><SidebarAccount collapsed onNavigate={onNavigate} /></footer>
          </div>
        )}

        <div id="chat-sidebar-content" className="n-sidebar-panel" inert={collapsed && !mobile}>
          <div className="n-sidebar-inner">
            <header className="n-sidebar-header">
              <Link to="/" state={newChatState} className="n-sidebar-brand" onClick={(event) => { if (!event.metaKey && !event.ctrlKey) onNavigate() }}>
                <span className="n-sidebar-logo" aria-hidden="true" />
                <span className="n-sidebar-brand-label">{t('app.name')}</span>
              </Link>
              <Tooltip label={t('chat.sidebar.close')} side="bottom" disabled={mobile}>
                <button type="button" className="n-sidebar-toggle" aria-label={t('chat.sidebar.close')} aria-expanded aria-controls="chat-sidebar-content" onClick={onToggle}>
                  <PanelLeft aria-hidden="true" />
                </button>
              </Tooltip>
            </header>

            <div ref={scroll} className="n-sidebar-scroll" onScroll={(event) => setScrolled(event.currentTarget.scrollTop > 0)}>
              <nav className="n-sidebar-nav" aria-label={t('chat.sidebar.navigation')}>
                <Link className="n-row n-sidebar-nav-row" to="/" state={newChatState} activeProps={{ className: '' }} onClick={(event) => { if (!event.metaKey && !event.ctrlKey) onNavigate() }} aria-keyshortcuts="Control+Shift+O Meta+Shift+O">
                  <SquarePen aria-hidden="true" />
                  <span className="n-row-label">{t('chat.nav.new')}</span>
                  <kbd className="n-sidebar-hotkey" aria-hidden="true">{modifier} ⇧ O</kbd>
                </Link>
                <button ref={collapsed ? undefined : searchTrigger} type="button" className="n-row n-sidebar-nav-row" onClick={openSearch} aria-keyshortcuts="Control+K Meta+K">
                  <Search aria-hidden="true" />
                  <span className="n-row-label">{t('chat.sidebar.search')}</span>
                  <kbd className="n-sidebar-hotkey" aria-hidden="true">{modifier} K</kbd>
                </button>
                <Link className="n-row n-sidebar-nav-row" to="/library" activeProps={{ className: 'active' }} onClick={(event) => { if (!event.metaKey && !event.ctrlKey) onNavigate() }}>
                  <LibraryBig aria-hidden="true" />
                  <span className="n-row-label">{t('library.title')}</span>
                </Link>
                {tasksOn && (
                  <Link className="n-row n-sidebar-nav-row" to="/tasks" activeProps={{ className: 'active' }} onClick={(event) => { if (!event.metaKey && !event.ctrlKey) onNavigate() }}>
                    <AlarmClock aria-hidden="true" />
                    <span className="n-row-label">{t('tasks.title')}</span>
                  </Link>
                )}
              </nav>

              <div className="n-sidebar-history">
                <Collapsible.Root open={chatsOpen} onOpenChange={setChatsOpen}>
                  <Collapsible.Trigger className="n-sidebar-section-toggle">
                    <span>{t('chat.sidebar.chats')}</span>
                    <ChevronRight aria-hidden="true" />
                  </Collapsible.Trigger>
                  <Collapsible.Content>
                    <div className="n-sidebar-groups">
                      {conversations.isPending ? (
                        <div className="n-sidebar-empty" role="status">{t('state.loading')}</div>
                      ) : conversations.isError ? (
                        <button type="button" className="n-sidebar-empty n-sidebar-retry" onClick={() => void conversations.refetch()}>{t('chat.sidebar.retry')}</button>
                      ) : groups.length === 0 ? (
                        <div className="n-sidebar-empty">{t('chat.conversation.empty')}</div>
                      ) : groups.map((group) => (
                        <section key={group.key} className="n-sidebar-group" aria-label={group.label}>
                          <h3 className="n-group">{group.label}</h3>
                          {group.items.map((conversation) => (
                            <SidebarConversation key={conversation.id} conversation={conversation} active={conversation.id === activeId} running={running.has(conversation.id)} onDelete={onDelete} onNavigate={onNavigate} />
                          ))}
                        </section>
                      ))}
                      {hasNextPage && (
                        <button ref={loadMore} type="button" className="n-sidebar-load-more" disabled={isFetchingNextPage} onClick={() => void fetchNextPage()}>
                          {isFetchingNextPage ? t('state.loading') : isFetchNextPageError ? t('chat.sidebar.retry') : t('chat.sidebar.load-more')}
                        </button>
                      )}
                    </div>
                  </Collapsible.Content>
                </Collapsible.Root>
              </div>
            </div>

            <footer className="n-sidebar-account"><SidebarAccount collapsed={false} onNavigate={onNavigate} /></footer>
          </div>
        </div>
        {!collapsed && !mobile && <SidebarResizeHandle width={dragWidth ?? width} onResize={setDragWidth} onCommit={setWidth} />}
      </aside>

      <SearchPalette
        host={searchHost}
        open={searchOpen}
        onOpenChange={setSearchOpen}
        conversations={history}
        hasMore={!!hasNextPage}
        loading={isFetchingNextPage}
        onLoadMore={() => void fetchNextPage()}
        onNavigate={onNavigate}
        onDelete={onDelete}
        onRestoreFocus={() => {
          const target = searchOpener.current?.isConnected ? searchOpener.current : searchTrigger.current
          target?.focus()
        }}
      />
    </TooltipGroup>
  )
}

export default Sidebar
