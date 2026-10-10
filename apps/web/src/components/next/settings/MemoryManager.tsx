import type { Memory, MemoryEvent } from '@openchat/api-client'
import { Check, ChevronDown, ChevronUp, Pencil, Search, Trash2, X } from 'lucide-react'
import { Dialog } from 'radix-ui'
import { useMemo, useRef, useState, type FormEvent } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import {
  useClearMemories,
  useCreateMemory,
  useDeleteMemory,
  useMemories,
  useMemoryEvents,
  usePersonalization,
  useUndoMemoryEvent,
  useUpdateMemory,
} from '@/hooks/use-memory'
import { useMemoryErrorText, useUndoErrorText } from '@/hooks/use-memory-errors'
import { cn } from '@/lib/utils'

type SortKey = 'content' | 'updated'

/**
 * The memory list in a modal of its own, after Open WebUI's Manage Memory: the
 * title with the count, a search, a header row to sort by, one line per memory
 * with its date and edit and delete marks, and at the foot "clear" and "add".
 * It sits over the settings dialog (a second, smaller one), and Escape closes
 * only the top one.
 *
 * What the model changed lately is listed above the memories with an undo, so
 * "recently learned" is one glance away.
 */
export function MemoryManager({
  host, open, onOpenChange,
}: { host: HTMLElement | null; open: boolean; onOpenChange: (open: boolean) => void }) {
  const { t } = useTranslation()
  const content = useRef<HTMLDivElement>(null)
  return (
    <Dialog.Root open={open} onOpenChange={onOpenChange}>
      <Dialog.Portal container={host}>
        <Dialog.Overlay className="n-modal-overlay is-top">
          <Dialog.Content
            ref={content}
            className="n-modal n-memory-modal"
            aria-describedby={undefined}
            onOpenAutoFocus={(event) => { event.preventDefault(); content.current?.focus() }}
          >
            <Dialog.Title className="sr-only">{t('settings.memory.title')}</Dialog.Title>
            <Manager onClose={() => onOpenChange(false)} />
          </Dialog.Content>
        </Dialog.Overlay>
      </Dialog.Portal>
    </Dialog.Root>
  )
}

function Manager({ onClose }: { onClose: () => void }) {
  const { t, i18n } = useTranslation()
  const { data: personalization } = usePersonalization()
  const memories = useMemories()
  const create = useCreateMemory()
  const clear = useClearMemories()
  const errorText = useMemoryErrorText()
  const [query, setQuery] = useState('')
  const [sort, setSort] = useState<{ key: SortKey; desc: boolean }>({ key: 'updated', desc: true })
  const [adding, setAdding] = useState(false)
  const [text, setText] = useState('')
  const [error, setError] = useState<string>()
  const [confirmClear, setConfirmClear] = useState(false)

  const items = memories.data
  const limit = personalization?.memoryLimit ?? 100
  const length = personalization?.memoryLength ?? 200
  const full = (items?.length ?? 0) >= limit

  const shown = useMemo(() => {
    const needle = query.trim().toLocaleLowerCase()
    const found = (items ?? []).filter((m) => !needle || m.content.toLocaleLowerCase().includes(needle))
    const order = sort.desc ? -1 : 1
    return found.sort((a, b) =>
      sort.key === 'content'
        ? a.content.localeCompare(b.content, i18n.language) * order
        : (Date.parse(a.updatedAt) - Date.parse(b.updatedAt)) * order,
    )
  }, [items, query, sort, i18n.language])

  const sortBy = (key: SortKey) => setSort((s) => (s.key === key ? { key, desc: !s.desc } : { key, desc: key === 'updated' }))

  const add = async (event: FormEvent) => {
    event.preventDefault()
    const content = text.trim()
    if (!content) return
    try {
      await create.mutateAsync(content)
      setText('')
      setError(undefined)
    } catch (failure) {
      setError(errorText(failure))
    }
  }

  return (
    <div className="n-memory-manager">
      <header className="n-modal-head">
        <div className="n-modal-title">
          {t('settings.memory.title')}
          {items && <span className="n-modal-count">{items.length}</span>}
        </div>
        <button type="button" className="n-settings-close" aria-label={t('settings.close')} onClick={onClose}>
          <X aria-hidden="true" />
        </button>
      </header>

      <div className="n-memory-body">
        <label className="n-memory-search">
          <Search aria-hidden="true" />
          <span className="sr-only">{t('settings.memory.search')}</span>
          <input
            value={query}
            maxLength={500}
            placeholder={t('settings.memory.search')}
            onChange={(e) => setQuery(e.target.value)}
          />
          {query && (
            <button type="button" className="n-memory-clear" aria-label={t('settings.memory.clear-search')} onClick={() => setQuery('')}>
              <X aria-hidden="true" />
            </button>
          )}
        </label>

        <RecentChanges />

        {full ? (
          <p className="n-settings-callout" role="status">
            {t('settings.memory.full', { max: limit })}
          </p>
        ) : (
          adding && (
            <form className="n-memory-add" onSubmit={(e) => void add(e)}>
              <input
                className="n-memory-add-input"
                value={text}
                maxLength={length}
                autoFocus
                placeholder={t('settings.memory.add-placeholder')}
                aria-label={t('settings.memory.add-placeholder')}
                aria-invalid={Boolean(error)}
                onChange={(e) => {
                  setText(e.target.value)
                  setError(undefined)
                }}
                onKeyDown={(e) => {
                  if (e.key === 'Escape') {
                    e.stopPropagation()
                    setAdding(false)
                  }
                }}
              />
              <button type="submit" className="n-settings-save" disabled={!text.trim() || create.isPending}>
                {t('settings.memory.add')}
              </button>
              <button type="button" className="n-settings-soft" onClick={() => setAdding(false)}>
                {t('settings.cancel')}
              </button>
            </form>
          )
        )}
        {adding && error && (
          <p className="n-settings-error" role="alert">
            {error}
          </p>
        )}

        {memories.isPending ? (
          <div className="n-settings-loading" role="status">
            <span className="n-spinner" data-motion="essential" />
          </div>
        ) : memories.isError ? (
          <div className="n-memory-none">
            {t('settings.load-failed')}
            <button type="button" className="n-settings-soft" onClick={() => void memories.refetch()}>
              {t('settings.retry')}
            </button>
          </div>
        ) : shown.length === 0 ? (
          <div className="n-memory-none">{(items?.length ?? 0) === 0 ? t('settings.memory.empty') : t('settings.memory.no-match')}</div>
        ) : (
          <>
            <div className="n-memory-cols">
              <button type="button" className="n-memory-col" onClick={() => sortBy('content')}>
                {t('settings.memory.content')}
                <SortMark active={sort.key === 'content'} desc={sort.desc} />
              </button>
              <button type="button" className="n-memory-col is-end" onClick={() => sortBy('updated')}>
                {t('settings.memory.updated-at')}
                <SortMark active={sort.key === 'updated'} desc={sort.desc} />
              </button>
            </div>
            <ul className="n-memory-rows">
              {shown.map((memory) => (
                <MemoryRow key={memory.id} memory={memory} max={length} />
              ))}
            </ul>
          </>
        )}
      </div>

      <footer className="n-memory-foot">
        {confirmClear ? (
          <span className="n-memory-confirm">
            <span>{t('settings.memory.clear-confirm')}</span>
            <button
              type="button"
              className="n-settings-danger"
              disabled={clear.isPending}
              onClick={() =>
                clear.mutate(undefined, {
                  onSuccess: () => {
                    setConfirmClear(false)
                    toast.success(t('settings.memory.cleared'))
                  },
                  onError: (failure) => toast.error(errorText(failure)),
                })
              }
            >
              {t('settings.memory.clear-yes')}
            </button>
            <button type="button" className="n-settings-soft" onClick={() => setConfirmClear(false)}>
              {t('settings.cancel')}
            </button>
          </span>
        ) : (
          <button
            type="button"
            className="n-memory-clearall"
            disabled={(items?.length ?? 0) === 0}
            onClick={() => setConfirmClear(true)}
          >
            {t('settings.memory.clear-memory')}
          </button>
        )}
        <button type="button" className="n-settings-outline" disabled={adding || full} onClick={() => setAdding(true)}>
          {t('settings.memory.add-memory')}
        </button>
      </footer>
    </div>
  )
}

function SortMark({ active, desc }: { active: boolean; desc: boolean }) {
  // Always drawn, hidden when not the sort, so the header does not shift.
  const Mark = desc ? ChevronDown : ChevronUp
  return <Mark className={cn('n-memory-sort', !active && 'is-hidden')} aria-hidden="true" />
}

function MemoryRow({ memory, max }: { memory: Memory; max: number }) {
  const { t, i18n } = useTranslation()
  const update = useUpdateMemory()
  const remove = useDeleteMemory()
  const errorText = useMemoryErrorText()
  const [draft, setDraft] = useState<string | null>(null)
  const [confirm, setConfirm] = useState(false)
  const [error, setError] = useState<string>()

  const when = new Date(memory.updatedAt)
  const day = new Intl.DateTimeFormat(i18n.language, { dateStyle: 'medium' }).format(when)
  const time = new Intl.DateTimeFormat(i18n.language, { timeStyle: 'short' }).format(when)

  const save = async (event: FormEvent) => {
    event.preventDefault()
    const content = (draft ?? '').trim()
    if (!content || content === memory.content) {
      setDraft(null)
      return
    }
    try {
      await update.mutateAsync({ id: memory.id, content })
      setDraft(null)
      setError(undefined)
    } catch (failure) {
      setError(errorText(failure))
    }
  }

  if (draft !== null) {
    return (
      <li className="n-memory-row is-editing">
        <form className="n-memory-edit" onSubmit={(e) => void save(e)}>
          <input
            className="n-memory-add-input"
            value={draft}
            maxLength={max}
            autoFocus
            aria-label={t('settings.memory.edit')}
            aria-invalid={Boolean(error)}
            onChange={(e) => {
              setDraft(e.target.value)
              setError(undefined)
            }}
            onKeyDown={(e) => {
              // Escape closes the modal too, unless it is spent putting this back.
              if (e.key === 'Escape') {
                e.stopPropagation()
                setDraft(null)
              }
            }}
          />
          <button type="submit" className="n-memory-icon" aria-label={t('settings.save')} disabled={update.isPending}>
            <Check aria-hidden="true" />
          </button>
          <button type="button" className="n-memory-icon" aria-label={t('settings.cancel')} onClick={() => setDraft(null)}>
            <X aria-hidden="true" />
          </button>
        </form>
        {error && (
          <p className="n-settings-error" role="alert">
            {error}
          </p>
        )}
      </li>
    )
  }

  if (confirm) {
    return (
      <li className="n-memory-row is-confirming">
        <div className="n-memory-main">
          <div className="n-memory-line">{memory.content}</div>
          <div className="n-memory-date">{t('settings.memory.delete-confirm')}</div>
        </div>
        <span className="n-memory-actions">
          <button
            type="button"
            className="n-settings-danger"
            disabled={remove.isPending}
            onClick={() => remove.mutate(memory.id, { onError: (failure) => toast.error(errorText(failure)) })}
          >
            {t('settings.memory.delete')}
          </button>
          <button type="button" className="n-settings-soft" onClick={() => setConfirm(false)}>
            {t('settings.cancel')}
          </button>
        </span>
      </li>
    )
  }

  return (
    <li className="n-memory-row">
      <button type="button" className="n-memory-main" onClick={() => setDraft(memory.content)}>
        <span className="n-memory-line">{memory.content}</span>
        <span className="n-memory-date">
          {day}
          {memory.source !== 'user' && ` · ${t(`settings.memory.source-${memory.source}`)}`}
        </span>
      </button>
      <span className="n-memory-time">{time}</span>
      <span className="n-memory-actions">
        <button type="button" className="n-memory-icon" aria-label={t('settings.memory.edit')} onClick={() => setDraft(memory.content)}>
          <Pencil aria-hidden="true" />
        </button>
        <button type="button" className="n-memory-icon" aria-label={t('settings.memory.delete')} onClick={() => setConfirm(true)}>
          <Trash2 aria-hidden="true" />
        </button>
      </span>
    </li>
  )
}

/** How many of the model's latest changes are listed. */
const RECENT = 5

function RecentChanges() {
  const { t } = useTranslation()
  const events = useMemoryEvents()
  const undo = useUndoMemoryEvent()
  const errorText = useUndoErrorText()

  const recent = (events.data ?? []).slice(0, RECENT)
  if (recent.length === 0) return null

  return (
    <section className="n-memory-recent" aria-label={t('settings.memory.recent')}>
      <div className="n-memory-recent-title">{t('settings.memory.recent')}</div>
      <ul>
        {recent.map((event) => (
          <li key={event.id} className={cn('n-memory-recent-row', event.undoneAt && 'is-undone')}>
            <span className="n-settings-badge">{t(`settings.memory.op-${event.op}`)}</span>
            <span className="n-memory-recent-text">{changeText(event)}</span>
            {event.undoneAt ? (
              <span className="n-memory-undone">{t('settings.memory.undone')}</span>
            ) : (
              <button
                type="button"
                className="n-settings-soft is-small"
                disabled={undo.isPending}
                onClick={() => undo.mutate(event, { onError: (failure) => toast.error(errorText(failure)) })}
              >
                {t('settings.memory.undo')}
              </button>
            )}
          </li>
        ))}
      </ul>
    </section>
  )
}

/** What a logged change did, in the words of the memory it touched. */
function changeText(event: MemoryEvent) {
  return (event.op === 'delete' ? event.before : event.after) ?? ''
}

export default MemoryManager
