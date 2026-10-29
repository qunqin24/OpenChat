import type { ConversationReference, LibraryFile } from '@openchat/api-client'
import { Camera, Check, ChevronLeft, ChevronRight, LibraryBig, MessagesSquare, Paperclip, Plus, Search } from 'lucide-react'
import { useEffect, useRef, useState, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { useConversations, useConversationSearch } from '@/hooks/use-conversations'
import { useDebounced } from '@/hooks/use-debounced'
import { useLibraryFiles } from '@/hooks/use-library'
import { canCaptureScreen, captureScreen, libraryFileAsFile } from '@/lib/attach-sources'
import { useDisplayTitle } from '@/lib/titles'
import { cn } from '@/lib/utils'
import { FileIcon } from './FileIcon'
import Tooltip from './Tooltip'

/** How many conversations one message can refer to (api/references.go). */
const MAX_REFERENCES = 5

type View = 'main' | 'library' | 'conversations'

/**
 * The composer's "+": a menu of ways to attach, after OpenWebUI's - a file
 * from the disk, a screenshot, a file already sent (the library), or another
 * conversation. The last two, as there, turn the menu itself into a list to
 * pick from (a way back, a search field, the list) rather than open a second
 * menu beside it. The files end in a File for `onAdd`
 * (lib/attach-sources.ts), so all of them meet the same checks as a picked
 * file; a conversation is referred to, not copied (`onRefer`;
 * ConversationChip). The lists load only when they are shown.
 */
export function AttachMenu({
  disabled,
  hint,
  conversationId,
  references,
  onPickFiles,
  onAdd,
  onRefer,
}: {
  disabled: boolean
  /** What the button does, or why it is off. */
  hint: string
  /** The conversation being written in, left out of the conversations offered. */
  conversationId?: string
  /** The conversations already referred to, shown as chosen. */
  references: ConversationReference[]
  onPickFiles: () => void
  onAdd: (files: File[]) => void
  onRefer: (reference: ConversationReference) => void
}) {
  const { t } = useTranslation()
  const [view, setView] = useState<View>('main')
  // Which way the view slides in: into a list from the right, back from the left.
  const [cameBack, setCameBack] = useState(false)

  // Fetching a file can take a moment; the toast says so, and says it if it
  // fails. Nothing is added until the File is whole.
  const attachFrom = async (name: string, load: () => Promise<File>) => {
    const id = toast.loading(t('chat.attach.loading', { name }))
    try {
      const file = await load()
      toast.dismiss(id)
      onAdd([file])
    } catch {
      toast.error(t('chat.attach.failed', { name }), { id })
    }
  }

  const screenshot = async () => {
    try {
      // "2026-10-29 15.30.12": sortable, and no colon, which some systems refuse in a name.
      const stamp = new Date().toLocaleString('sv').replace(/:/g, '.')
      const file = await captureScreen(`${t('chat.attach.screenshot-name')} ${stamp}`)
      if (file) onAdd([file])
    } catch {
      toast.error(t('chat.attach.screenshot-failed'))
    }
  }

  // An item that turns the menu into one of its lists, leaving it open.
  const opens = (next: View) => (event: Event) => {
    event.preventDefault()
    setCameBack(false)
    setView(next)
  }
  const back = () => {
    setCameBack(true)
    setView('main')
  }

  return (
    // Opened again, it starts from the top.
    <DropdownMenu
      onOpenChange={(open) => {
        if (open) return
        setView('main')
        setCameBack(false)
      }}
    >
      {/* A disabled trigger fires no pointer events, so the tooltip would go
          silent exactly when the reader most needs to know why the button is
          dead — hence the wrapper span picking the hover up for it. */}
      <Tooltip label={hint}>
        <span className="n-tip-wrap">
          <DropdownMenuTrigger asChild disabled={disabled}>
            <button type="button" className="n-icon-btn" disabled={disabled} aria-label={t('chat.attach.menu')}>
              <Plus className="h-[18px] w-[18px]" />
            </button>
          </DropdownMenuTrigger>
        </span>
      </Tooltip>
      <DropdownMenuContent
        className="n-attach-menu"
        align="start"
        side="top"
        // Closed, the menu would hand focus back to "+", whose tooltip then
        // opens over the box: what comes next is writing, so focus goes there
        // - but not on a touch screen, where it would bring the keyboard up.
        onCloseAutoFocus={(event) => {
          event.preventDefault()
          if (!window.matchMedia('(pointer: coarse)').matches) document.getElementById('input')?.focus()
        }}
      >
        {view === 'main' && (
          <div className={cn('n-attach-view', cameBack && 'is-back')}>
            <DropdownMenuItem onSelect={onPickFiles}>
              <Paperclip aria-hidden="true" />
              <span>{t('chat.attach.upload')}</span>
            </DropdownMenuItem>
            {canCaptureScreen() && (
              <DropdownMenuItem onSelect={() => void screenshot()}>
                <Camera aria-hidden="true" />
                <span>{t('chat.attach.screenshot')}</span>
              </DropdownMenuItem>
            )}
            <DropdownMenuItem onSelect={opens('library')}>
              <LibraryBig aria-hidden="true" />
              <span>{t('chat.attach.library')}</span>
              <ChevronRight className="n-attach-more" aria-hidden="true" />
            </DropdownMenuItem>
            <DropdownMenuItem onSelect={opens('conversations')}>
              <MessagesSquare aria-hidden="true" />
              <span>{t('chat.attach.conversation')}</span>
              <ChevronRight className="n-attach-more" aria-hidden="true" />
            </DropdownMenuItem>
          </div>
        )}
        {view === 'library' && (
          <LibraryChoices
            onBack={back}
            onPick={(file) => void attachFrom(file.filename, () => libraryFileAsFile(file))}
          />
        )}
        {view === 'conversations' && (
          <ConversationChoices
            onBack={back}
            exclude={conversationId}
            chosen={references.map((r) => r.conversationId)}
            onPick={(reference) => {
              if (references.length >= MAX_REFERENCES) toast.error(t('chat.attach.too-many'))
              else onRefer(reference)
            }}
          />
        )}
      </DropdownMenuContent>
    </DropdownMenu>
  )
}

/**
 * One of the menu's lists: a way back with the list's name, a search field,
 * and what is found. The field takes the keys a menu would otherwise use to
 * jump to an item by its first letter - wherever the pointer has left the
 * focus, typing goes to the field - and the down arrow moves into the list.
 */
function ChoicePanel({
  title,
  placeholder,
  typed,
  onType,
  onBack,
  onEnd,
  children,
}: {
  title: string
  placeholder: string
  typed: string
  onType: (value: string) => void
  onBack: () => void
  /** The list was scrolled to its end: the next page, if there is one. */
  onEnd: () => void
  children: ReactNode
}) {
  const { t } = useTranslation()
  const field = useRef<HTMLInputElement>(null)
  const list = useRef<HTMLDivElement>(null)

  // Shown, the field is where typing goes - once the menu has finished
  // putting focus back on itself after the item that led here.
  useEffect(() => {
    const frame = requestAnimationFrame(() => field.current?.focus({ preventScroll: true }))
    return () => cancelAnimationFrame(frame)
  }, [])

  return (
    <div
      className="n-attach-view n-attach-panel"
      onKeyDownCapture={(event) => {
        if (event.target === field.current) return
        const printable = event.key.length === 1 && !event.metaKey && !event.ctrlKey && !event.altKey
        if (printable || event.key === 'Backspace') {
          event.stopPropagation()
          field.current?.focus({ preventScroll: true })
        }
      }}
    >
      <DropdownMenuItem
        aria-label={t('chat.attach.back')}
        onSelect={(event) => {
          event.preventDefault()
          onBack()
        }}
      >
        <ChevronLeft aria-hidden="true" />
        <span>{title}</span>
      </DropdownMenuItem>
      <div className="n-attach-search">
        <Search aria-hidden="true" />
        <input
          ref={field}
          type="search"
          value={typed}
          placeholder={placeholder}
          aria-label={placeholder}
          onChange={(event) => onType(event.target.value)}
          onKeyDown={(event) => {
            if (event.key === 'Escape' || event.key === 'Tab') return
            event.stopPropagation()
            if (event.key === 'ArrowDown') {
              event.preventDefault()
              list.current?.querySelector<HTMLElement>('[role="menuitem"]:not([data-disabled])')?.focus()
            } else if (event.key === 'Backspace' && typed === '') {
              onBack()
            }
          }}
        />
      </div>
      <div
        className="n-attach-list"
        ref={list}
        onScroll={(event) => {
          const box = event.currentTarget
          if (box.scrollHeight - box.scrollTop - box.clientHeight < 40) onEnd()
        }}
      >
        {children}
      </div>
    </div>
  )
}

/** Files already sent, the latest or those whose name matches. */
function LibraryChoices({ onBack, onPick }: { onBack: () => void; onPick: (file: LibraryFile) => void }) {
  const { t } = useTranslation()
  const [typed, setTyped] = useState('')
  const term = useDebounced(typed.trim(), 200)
  const library = useLibraryFiles(term, 'all')
  const files = (library.data?.pages ?? []).flatMap((page) => page.items)

  return (
    <ChoicePanel
      title={t('chat.attach.library-title')}
      placeholder={t('chat.attach.library-search')}
      typed={typed}
      onType={setTyped}
      onBack={onBack}
      onEnd={() => library.hasNextPage && !library.isFetchingNextPage && void library.fetchNextPage()}
    >
      {library.isPending ? (
        <Note text={t('chat.attach.loading-list')} />
      ) : files.length === 0 ? (
        <Note text={term ? t('chat.attach.no-file-match') : t('chat.attach.library-empty')} />
      ) : (
        files.map((file) => (
          <DropdownMenuItem key={file.id} title={file.filename} onSelect={() => onPick(file)}>
            <FileIcon filename={file.filename} contentType={file.contentType} className="n-attach-file-icon" />
            <span>{file.filename}</span>
          </DropdownMenuItem>
        ))
      )}
    </ChoicePanel>
  )
}

/**
 * The conversations but this one: the latest, from the sidebar's list, or
 * those a search finds (by title or by anything said in them, as the search
 * palette's).
 */
function ConversationChoices({
  onBack,
  exclude,
  chosen,
  onPick,
}: {
  onBack: () => void
  exclude?: string
  chosen: string[]
  onPick: (reference: ConversationReference) => void
}) {
  const { t } = useTranslation()
  const displayTitle = useDisplayTitle()
  const [typed, setTyped] = useState('')
  const term = useDebounced(typed.trim(), 200)
  const latest = useConversations()
  const search = useConversationSearch(term)
  const list = term ? search : latest
  const conversations = (list.data?.pages ?? [])
    .flatMap((page) => page.items)
    .filter((conversation) => conversation.id !== exclude)

  return (
    <ChoicePanel
      title={t('chat.attach.conversations-title')}
      placeholder={t('chat.attach.search')}
      typed={typed}
      onType={setTyped}
      onBack={onBack}
      onEnd={() => list.hasNextPage && !list.isFetchingNextPage && void list.fetchNextPage()}
    >
      {list.isPending ? (
        <Note text={t('chat.attach.loading-list')} />
      ) : conversations.length === 0 ? (
        <Note text={term ? t('chat.attach.no-match') : t('chat.attach.conversations-empty')} />
      ) : (
        conversations.map((conversation) => {
          const title = displayTitle(conversation.title)
          const isChosen = chosen.includes(conversation.id)
          return (
            <DropdownMenuItem
              key={conversation.id}
              title={title}
              disabled={isChosen}
              onSelect={() => onPick({ conversationId: conversation.id, title: conversation.title })}
            >
              <span>{title}</span>
              {isChosen && <Check className="n-attach-chosen" aria-label={t('chat.attach.referred')} />}
            </DropdownMenuItem>
          )
        })
      )}
    </ChoicePanel>
  )
}

/** Instead of the list: still loading, or nothing in it. */
function Note({ text }: { text: string }) {
  return <div className="n-attach-note">{text}</div>
}
