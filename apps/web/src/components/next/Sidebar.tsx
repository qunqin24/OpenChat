import type { Conversation } from '@openchat/api-client'
import { Link, useNavigate } from '@tanstack/react-router'
import { ChevronRight, PanelLeft, Search, SquarePen } from 'lucide-react'
import { Collapsible } from 'radix-ui'
import { useCallback, useEffect, useMemo, useRef, useState, type CSSProperties } from 'react'
import { useTranslation } from 'react-i18next'

import { useConversations } from '@/hooks/use-conversations'
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
  const groups = useMemo(
    () => groupConversations(history, t, i18n.resolvedLanguage),
    [history, t, i18n.resolvedLanguage],
  )

  const openSearch = useCallback(() => {
    searchOpener.current = document.activeElement instanceof HTMLElement ? document.activeElement : null
    setSearchHost(scroll.current?.closest<HTMLElement>('.prism-next') ?? null)
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
        void navigate({ to: '/' })
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
        className={cn('n-sidebar', collapsed && 'collapsed', scrolled && 'has-scrolled', dragWidth !== null && 'resizing')}
        style={sidebarStyle}
        aria-label={t('next.sidebar.history')}
      >
        <div className="n-sidebar-inner">
          <header className="n-sidebar-header">
            <Link to="/" className="n-sidebar-brand" tabIndex={collapsed ? -1 : undefined} aria-hidden={collapsed || undefined} onClick={(event) => { if (!event.metaKey && !event.ctrlKey) onNavigate() }}>
              <span className="n-sidebar-logo" aria-hidden="true" />
              <span className="n-sidebar-brand-label">{t('app.name')}</span>
            </Link>
            <Tooltip label={collapsed ? t('next.sidebar.open') : t('next.sidebar.close')} side={collapsed ? 'right' : 'bottom'} disabled={mobile}>
              <button type="button" className="n-sidebar-toggle" aria-label={collapsed ? t('next.sidebar.open') : t('next.sidebar.close')} aria-expanded={!collapsed} aria-controls="chat-sidebar-content" onClick={onToggle}>
                <span className="n-sidebar-rail-logo" aria-hidden="true" />
                <PanelLeft aria-hidden="true" />
              </button>
            </Tooltip>
          </header>

          <div ref={scroll} className="n-sidebar-scroll" onScroll={(event) => setScrolled(event.currentTarget.scrollTop > 0)}>
            <nav className="n-sidebar-nav" aria-label={t('next.sidebar.navigation')}>
              <Tooltip label={t('next.nav.new')} side="right" disabled={!collapsed || mobile}>
                <Link className="n-row n-sidebar-nav-row" to="/" activeProps={{ className: '' }} onClick={(event) => { if (!event.metaKey && !event.ctrlKey) onNavigate() }} aria-label={t('next.nav.new')} aria-keyshortcuts="Control+Shift+O Meta+Shift+O">
                  <SquarePen aria-hidden="true" />
                  <span className="n-row-label">{t('next.nav.new')}</span>
                  <kbd className="n-sidebar-hotkey" aria-hidden="true">{modifier} ⇧ O</kbd>
                </Link>
              </Tooltip>
              <Tooltip label={t('next.sidebar.search')} side="right" disabled={!collapsed || mobile}>
                <button ref={searchTrigger} type="button" className="n-row n-sidebar-nav-row" onClick={openSearch} aria-label={t('next.sidebar.search')} aria-keyshortcuts="Control+K Meta+K">
                  <Search aria-hidden="true" />
                  <span className="n-row-label">{t('next.sidebar.search')}</span>
                  <kbd className="n-sidebar-hotkey" aria-hidden="true">{modifier} K</kbd>
                </button>
              </Tooltip>
            </nav>

            <div id="chat-sidebar-content" className="n-sidebar-history" hidden={collapsed}>
              <Collapsible.Root open={chatsOpen} onOpenChange={setChatsOpen}>
                <Collapsible.Trigger className="n-sidebar-section-toggle">
                  <span>{t('next.sidebar.chats')}</span>
                  <ChevronRight aria-hidden="true" />
                </Collapsible.Trigger>
                <Collapsible.Content>
                  <div className="n-sidebar-groups">
                    {conversations.isPending ? (
                      <div className="n-sidebar-empty" role="status">{t('state.loading')}</div>
                    ) : conversations.isError ? (
                      <button type="button" className="n-sidebar-empty n-sidebar-retry" onClick={() => void conversations.refetch()}>{t('next.sidebar.retry')}</button>
                    ) : groups.length === 0 ? (
                      <div className="n-sidebar-empty">{t('next.conversation.empty')}</div>
                    ) : groups.map((group) => (
                      <section key={group.key} className="n-sidebar-group" aria-label={group.label}>
                        <h3 className="n-group">{group.label}</h3>
                        {group.items.map((conversation) => (
                          <SidebarConversation key={conversation.id} conversation={conversation} active={conversation.id === activeId} onDelete={onDelete} onNavigate={onNavigate} />
                        ))}
                      </section>
                    ))}
                    {hasNextPage && (
                      <button ref={loadMore} type="button" className="n-sidebar-load-more" disabled={isFetchingNextPage} onClick={() => void fetchNextPage()}>
                        {isFetchingNextPage ? t('state.loading') : isFetchNextPageError ? t('next.sidebar.retry') : t('next.sidebar.load-more')}
                      </button>
                    )}
                  </div>
                </Collapsible.Content>
              </Collapsible.Root>
            </div>
          </div>

          <footer className="n-sidebar-account"><SidebarAccount collapsed={collapsed} onNavigate={onNavigate} /></footer>
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
