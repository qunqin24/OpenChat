import { Brain, Undo2 } from 'lucide-react'
import { useEffect, useRef } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { useUndoErrorText } from '@/hooks/use-memory-errors'
import { useMemoryEvents, useUndoMemoryEvent } from '@/hooks/use-memory'
import type { MemoryChange } from '@/lib/memory-tool'
import { useSettings } from '@/lib/settings'
import { cn } from '@/lib/utils'

/**
 * What the model just did to the person's memory, in one line of its reply:
 * "memory updated", the note, an undo and a way to the list. The log entry the
 * call made says whether it has been undone (and whether it still can be);
 * this reads it from the memory events query rather than from the transcript,
 * which never changes after the fact.
 */
export function MemoryChip({ change }: { change: MemoryChange }) {
  const { t } = useTranslation()
  const settings = useSettings()
  const events = useMemoryEvents()
  const undo = useUndoMemoryEvent()
  const undoError = useUndoErrorText()

  const event = events.data?.find((e) => e.id === change.eventId)
  const undone = Boolean(event?.undoneAt)

  // A change made a moment ago may not be in what was fetched earlier: ask once.
  const asked = useRef(false)
  useEffect(() => {
    if (!event && !events.isFetching && events.isSuccess && !asked.current) {
      asked.current = true
      void events.refetch()
    }
  }, [event, events])

  // An update shows what the note says now, not the old wording struck through
  // beside it: two near-identical paragraphs in a reply read as noise, and the
  // old one is a click away (Undo, or the manager's recent changes).
  const note = change.op === 'delete' ? change.before : change.content
  return (
    <div className={cn('n-memory-chip', undone && 'is-undone')} role="status">
      <Brain className="n-memory-chip-icon" aria-hidden="true" />
      <span className="n-memory-chip-label">{t(`chat.memory.${change.op}`)}</span>
      <span className="n-memory-chip-text">
        <span className={cn(change.op === 'delete' && 'is-removed')}>{note}</span>
      </span>
      {undone ? (
        <span className="n-memory-chip-state">{t('chat.memory.undone')}</span>
      ) : (
        event && (
          <button
            type="button"
            className="n-memory-chip-btn"
            disabled={undo.isPending}
            onClick={() => undo.mutate(event, { onError: (failure) => toast.error(undoError(failure)) })}
          >
            <Undo2 aria-hidden="true" />
            {t('chat.memory.undo')}
          </button>
        )
      )}
      <button type="button" className="n-memory-chip-btn" onClick={() => settings.open('personalization')}>
        {t('chat.memory.manage')}
      </button>
    </div>
  )
}

export default MemoryChip
