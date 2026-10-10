import type { TFunction } from 'i18next'
import type { Conversation } from '@openchat/api-client'
import { useNavigate } from '@tanstack/react-router'
import { Command } from 'cmdk'
import { Check, MoreHorizontal, PencilLine, Search, SquarePen, Trash2, X } from 'lucide-react'
import { Dialog } from 'radix-ui'
import { useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'

import {
  DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { useChatModels } from '@/hooks/use-chat-models'
import { useConversationSearch, useRenameConversation } from '@/hooks/use-conversations'
import { useMessages } from '@/hooks/use-messages'
import { API_BASE_URL } from '@/lib/api'
import { useChatPaths } from '@/lib/chat-paths'
import { groupConversations, parseTimestamp } from '@/lib/grouping'
import { displayTitle, useDisplayTitle } from '@/lib/titles'
import { buildTurns, type AssistantTurn as Turn } from '@/lib/transcript'
import { ImageSource, proxiedImages } from './genui/image-source'
import { AssistantTurn, UserMessage } from './MessageItem'

interface Props {
  /** The `.prism-next` element to render into: the one the sidebar is in. */
  host: HTMLElement | null
  open: boolean
  onOpenChange: (open: boolean) => void
  conversations: Conversation[]
  hasMore: boolean
  loading: boolean
  onLoadMore: () => void
  onNavigate: () => void
  onDelete: (conversation: Conversation) => void
  onRestoreFocus: () => void
}

/** cmdk's value for the one action, kept apart from conversation ids. */
const NEW_CHAT = 'action:new'

/**
 * Search, as a palette: actions first, then every conversation by date, and
 * beside them a read-only preview of whichever one the cursor is on.
 *
 * Typing searches on the server, titles and everything said alike, and a
 * conversation found by its words shows the passage under its title; the
 * preview marks the words and scrolls to the first place they appear. Until
 * the server answers the first time, the loaded titles are filtered here so
 * the list responds to the first keystroke.
 *
 * Portalled into the sidebar's own `.prism-next` scope rather than <body>, so
 * the ported `.n-palette*` styles and the transcript's styles apply inside it —
 * and, on a phone, so it opens over the sidebar drawer it was opened from
 * rather than behind it.
 */
export function SearchPalette({
  host, open, onOpenChange, conversations, hasMore, loading, onLoadMore, onNavigate, onDelete, onRestoreFocus,
}: Props) {
  const { t, i18n } = useTranslation()
  const navigate = useNavigate()
  const paths = useChatPaths()
  const [query, setQuery] = useState('')
  const [cursor, setCursor] = useState('')
  const [renaming, setRenaming] = useState<string | null>(null)
  const results = useRef<HTMLDivElement>(null)
  const field = useRef<HTMLInputElement>(null)
  const sentinel = useRef<HTMLDivElement>(null)

  const typed = query.trim()
  const term = useDebounced(typed, 200)
  const search = useConversationSearch(term)
  const found = useMemo(() => search.data?.pages.flatMap((page) => page.items), [search.data])
  const matches = useMemo(() => {
    if (!typed) return conversations
    if (found) return found
    const needle = typed.toLocaleLowerCase()
    return conversations.filter((c) => displayTitle(c.title, t).toLocaleLowerCase().includes(needle))
  }, [conversations, typed, found, t])
  // What the list is paging through: the search's pages while there is one.
  const searching = typed !== ''
  const more = searching ? !!search.hasNextPage : hasMore
  const loadingMore = searching ? search.isFetching : loading
  const loadMore = searching ? () => void search.fetchNextPage() : onLoadMore
  // The words shown in the results — the ones the server searched for.
  const marked = searching && found ? term : ''
  const groups = useMemo(
    () => groupConversations(matches, t, i18n.resolvedLanguage),
    [matches, t, i18n.resolvedLanguage],
  )

  const close = () => {
    setQuery('')
    setRenaming(null)
    onOpenChange(false)
  }
  const go = (conversationId?: string) => {
    close()
    onNavigate()
    if (conversationId) void navigate({ to: paths.chat, params: { conversationId } })
    else void navigate({ to: paths.home })
  }

  // The older conversations load as the list is scrolled to its end — and a
  // search that matches nothing loaded yet keeps loading until it finds some.
  useEffect(() => {
    const target = sentinel.current
    if (!open || !target || !more || loadingMore) return
    const observer = new IntersectionObserver(([entry]) => {
      if (entry?.isIntersecting) loadMore()
    }, { root: results.current, rootMargin: '0px 0px 120px 0px' })
    observer.observe(target)
    return () => observer.disconnect()
    // loadMore is a new function each render; what it does follows `searching`.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open, more, loadingMore, searching, matches.length])

  const previewId = cursor && cursor !== NEW_CHAT && matches.some((c) => c.id === cursor) ? cursor : null

  return (
    <Dialog.Root open={open} onOpenChange={(value) => { if (!value) close(); else onOpenChange(true) }}>
      <Dialog.Portal container={host}>
        <Dialog.Overlay className="n-palette-overlay">
          <Dialog.Content
            className="n-palette"
            onCloseAutoFocus={(event) => { event.preventDefault(); onRestoreFocus() }}
          >
            <Dialog.Title className="sr-only">{t('next.sidebar.search')}</Dialog.Title>
            <Dialog.Description className="sr-only">{t('next.search.description')}</Dialog.Description>
            <Command label={t('next.sidebar.search')} shouldFilter={false} loop value={cursor} onValueChange={setCursor}>
              <div className="n-palette-head">
                <Search className="n-palette-icon" aria-hidden="true" />
                <Command.Input
                  ref={field}
                  className="n-palette-input"
                  value={query}
                  onValueChange={setQuery}
                  placeholder={t('next.search.placeholder')}
                />
                {query && (
                  <button type="button" className="n-palette-clear" aria-label={t('next.search.clear')} onClick={() => setQuery('')}>
                    <X aria-hidden="true" />
                  </button>
                )}
              </div>
              <div className="n-palette-body">
                <Command.List ref={results} className="n-palette-results">
                  <Command.Group heading={t('next.search.actions')} className="n-palette-group">
                    <Command.Item value={NEW_CHAT} className="n-palette-item" onSelect={() => go()}>
                      <SquarePen className="n-palette-item-icon" aria-hidden="true" />
                      <span className="n-palette-title">{t('next.search.new-chat')}</span>
                    </Command.Item>
                  </Command.Group>

                  {groups.map((group) => (
                    <Command.Group key={group.key} heading={group.label} className="n-palette-group">
                      {group.items.map((conversation) => (
                        <PaletteRow
                          key={conversation.id}
                          conversation={conversation}
                          term={marked}
                          renaming={renaming === conversation.id}
                          onRename={(on) => setRenaming(on ? conversation.id : null)}
                          onOpen={() => go(conversation.id)}
                          onDelete={() => onDelete(conversation)}
                          onMenuClosed={() => field.current?.focus()}
                        />
                      ))}
                    </Command.Group>
                  ))}

                  {matches.length === 0 && !more && !(searching && (!found || search.isFetching)) && (
                    <div className="n-palette-empty">{t('next.sidebar.search-empty')}</div>
                  )}
                  {more && (
                    <div ref={sentinel} className="n-palette-more" role="status">
                      {loadingMore ? t('state.loading') : null}
                    </div>
                  )}
                </Command.List>
                <Preview conversationId={previewId} term={marked} />
              </div>
            </Command>
          </Dialog.Content>
        </Dialog.Overlay>
      </Dialog.Portal>
    </Dialog.Root>
  )
}

interface RowProps {
  conversation: Conversation
  /** The search, to mark in the title and the passage; empty for none. */
  term: string
  renaming: boolean
  onRename: (on: boolean) => void
  onOpen: () => void
  onDelete: () => void
  onMenuClosed: () => void
}

function PaletteRow({ conversation, term, renaming, onRename, onOpen, onDelete, onMenuClosed }: RowProps) {
  const { t, i18n } = useTranslation()
  const display = useDisplayTitle()
  const rename = useRenameConversation()
  const [title, setTitle] = useState(display(conversation.title))
  const input = useRef<HTMLInputElement>(null)
  // What the menu closes into: a rename, whose field takes the focus, or a
  // delete, whose confirmation opens once focus is back in the search field —
  // so that is where it returns when the confirmation closes.
  const after = useRef<'rename' | 'delete' | null>(null)

  useEffect(() => {
    if (renaming) { input.current?.focus(); input.current?.select() }
  }, [renaming])

  const save = async () => {
    const next = title.trim()
    if (!next || next === display(conversation.title)) { onRename(false); return }
    try {
      await rename.mutateAsync({ conversationId: conversation.id, title: next })
      onRename(false)
    } catch { input.current?.focus() }
  }

  return (
    <Command.Item
      value={conversation.id}
      className="n-palette-item"
      // Not `disabled`: that marks the field inside it disabled too. A click
      // into the field reaches here and must not open the conversation.
      onSelect={() => { if (!renaming) onOpen() }}
    >
      {renaming ? (
        <form className="n-palette-rename" onSubmit={(event) => { event.preventDefault(); void save() }}>
          <input
            ref={input}
            value={title}
            maxLength={200}
            aria-label={t('chat.rename')}
            aria-invalid={rename.isError || undefined}
            disabled={rename.isPending}
            onChange={(event) => setTitle(event.target.value)}
            // The palette reads arrows and Enter as navigation; while the
            // title is being edited they belong to the field.
            onKeyDown={(event) => {
              event.stopPropagation()
              if (event.nativeEvent.isComposing) return
              if (event.key === 'Escape') { event.preventDefault(); onRename(false) }
            }}
            onBlur={() => { if (!rename.isPending) void save() }}
          />
          <button type="submit" className="n-palette-row-button" aria-label={t('next.sidebar.save')} onMouseDown={(event) => event.preventDefault()}>
            <Check aria-hidden="true" />
          </button>
        </form>
      ) : (
        <span className="n-palette-text">
          <span className="n-palette-title" dir="auto">
            <Marked text={display(conversation.title)} term={term} />
          </span>
          {conversation.match && (
            <span className="n-palette-excerpt" dir="auto">
              <Marked text={conversation.match.excerpt} term={term} />
            </span>
          )}
        </span>
      )}
      <span className="n-palette-date">{formatDay(conversation.updatedAt, t, i18n.resolvedLanguage)}</span>
      {!renaming && (
        <DropdownMenu modal={false}>
          <DropdownMenuTrigger asChild>
            <button
              type="button"
              className="n-palette-row-button"
              aria-label={t('next.more')}
              // Not a click on the row, which would open the conversation.
              onClick={(event) => event.stopPropagation()}
              onPointerDown={(event) => event.stopPropagation()}
            >
              <MoreHorizontal aria-hidden="true" />
            </button>
          </DropdownMenuTrigger>
          <DropdownMenuContent
            align="end"
            onKeyDown={(event) => event.stopPropagation()}
            onClick={(event) => event.stopPropagation()}
            // Back to the field, not the menu button: there the arrows and
            // Enter move through the list again, as they did before.
            onCloseAutoFocus={(event) => {
              event.preventDefault()
              if (after.current !== 'rename') onMenuClosed()
              if (after.current === 'delete') onDelete()
              after.current = null
            }}
          >
            <DropdownMenuItem onSelect={() => { after.current = 'rename'; setTitle(display(conversation.title)); rename.reset(); onRename(true) }}>
              <PencilLine /><span>{t('chat.rename')}</span>
            </DropdownMenuItem>
            <DropdownMenuItem onSelect={() => { after.current = 'delete' }}><Trash2 /><span>{t('chat.delete')}</span></DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
      )}
    </Command.Item>
  )
}

/**
 * The conversation under the cursor, read-only and scrolled to its end, where
 * it left off. Fetched after the cursor rests for a moment, so running the
 * arrow keys down the list does not load every conversation it passes.
 */
function Preview({ conversationId, term }: { conversationId: string | null; term: string }) {
  const { t } = useTranslation()
  const [shown, setShown] = useState(conversationId)
  const scroll = useRef<HTMLDivElement>(null)
  const models = useChatModels()
  const messages = useMessages(shown ?? '')

  useEffect(() => {
    const timer = window.setTimeout(() => setShown(conversationId), conversationId ? 120 : 0)
    return () => window.clearTimeout(timer)
  }, [conversationId])

  const turns = useMemo(() => buildTurns(messages.data?.items ?? [], null), [messages.data])
  const modelById = useMemo(() => new Map((models.data ?? []).map((m) => [m.id, m])), [models.data])

  // To the end, where the conversation left off — or, for a search, to the
  // first place the words appear, marked with the CSS highlight API so the
  // transcript's own DOM is left alone. A match that is not drawn has nothing
  // to mark, and the end is where it stays.
  useLayoutEffect(() => {
    const el = scroll.current
    if (!el) return
    el.scrollTop = el.scrollHeight
    const ranges = term ? rangesOf(el, term) : []
    const first = ranges[0]?.startContainer.parentElement
    if (first) {
      const top = first.getBoundingClientRect().top - el.getBoundingClientRect().top + el.scrollTop
      el.scrollTop = Math.max(0, top - el.clientHeight / 3)
    }
    if (ranges.length === 0 || typeof CSS === 'undefined' || !('highlights' in CSS)) return
    CSS.highlights.set(HIGHLIGHT, new Highlight(...ranges))
    return () => { CSS.highlights.delete(HIGHLIGHT) }
  }, [turns, term])

  return (
    <div ref={scroll} className="n-palette-preview" aria-hidden="true">
      {!shown ? (
        <div className="n-palette-preview-idle">{t('next.search.preview-idle')}</div>
      ) : messages.isPending ? (
        <div className="n-palette-preview-idle"><span className="n-spinner" data-motion="essential" /></div>
      ) : turns.length === 0 ? (
        <div className="n-palette-preview-idle">{t('next.search.preview-empty')}</div>
      ) : (
        // Inert: a preview to read, not a transcript to act on.
        <ImageSource.Provider value={proxiedImages(API_BASE_URL, shown)}>
          <div className="n-palette-transcript" inert>
            {turns.map((turn) =>
              turn.kind === 'user' ? (
                <UserMessage key={turn.key} message={turn.message} />
              ) : (
                <AssistantTurn key={turn.key} turn={turn} modelId={turnModel(turn) ?? ''} model={modelById.get(turnModel(turn) ?? '')} />
              ),
            )}
          </div>
        </ImageSource.Provider>
      )}
    </div>
  )
}

const HIGHLIGHT = 'n-search'

/** Every occurrence of term in root's text, ignoring case. */
function rangesOf(root: HTMLElement, term: string): Range[] {
  const needle = term.toLocaleLowerCase()
  const ranges: Range[] = []
  const walker = document.createTreeWalker(root, NodeFilter.SHOW_TEXT)
  for (let node = walker.nextNode(); node && ranges.length < 200; node = walker.nextNode()) {
    const text = (node.nodeValue ?? '').toLocaleLowerCase()
    for (let at = text.indexOf(needle); at !== -1; at = text.indexOf(needle, at + needle.length)) {
      const range = document.createRange()
      range.setStart(node, at)
      range.setEnd(node, at + needle.length)
      ranges.push(range)
    }
  }
  return ranges
}

/** text with every occurrence of term, ignoring case, in a <mark>. */
function Marked({ text, term }: { text: string; term: string }) {
  if (!term) return <>{text}</>
  const lower = text.toLocaleLowerCase()
  const needle = term.toLocaleLowerCase()
  // Lowercasing that changes a string's length (rare, e.g. 'İ') would put the
  // marks in the wrong place; better none.
  if (lower.length !== text.length) return <>{text}</>
  const out: React.ReactNode[] = []
  let from = 0
  for (let at = lower.indexOf(needle); at !== -1; at = lower.indexOf(needle, from)) {
    if (at > from) out.push(text.slice(from, at))
    out.push(<mark key={at} className="n-palette-mark">{text.slice(at, at + needle.length)}</mark>)
    from = at + needle.length
  }
  if (from < text.length) out.push(text.slice(from))
  return <>{out}</>
}

/** value, once it has stopped changing for `delay` ms. */
function useDebounced<T>(value: T, delay: number): T {
  const [settled, setSettled] = useState(value)
  useEffect(() => {
    const timer = window.setTimeout(() => setSettled(value), delay)
    return () => window.clearTimeout(timer)
  }, [value, delay])
  return settled
}

function turnModel(turn: Turn) {
  return turn.responses.find((m) => m.model)?.model
}

type T = TFunction<'common'>

/** 今天 / 昨天 / 2026/09/26 — a day, not a duration. */
function formatDay(value: string | undefined, t: T, locale?: string): string {
  const date = parseTimestamp(value)
  if (!date) return ''
  const day = (d: Date) => new Date(d.getFullYear(), d.getMonth(), d.getDate()).getTime()
  const days = Math.round((day(new Date()) - day(date)) / 86_400_000)
  if (days <= 0) return t('next.group.today')
  if (days === 1) return t('next.group.yesterday')
  return new Intl.DateTimeFormat(locale, { year: 'numeric', month: '2-digit', day: '2-digit' }).format(date)
}

export default SearchPalette
