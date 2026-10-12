import type { ContextSummary } from '@openchat/api-client'
import { useId, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Markdown } from './MarkdownText'

interface CompactionDividerProps {
  summary: ContextSummary
  /**
   * Takes the compression back. Absent while that cannot be done (a run is
   * going: the context would change between two of its requests).
   */
  onUndo?: () => void
  undoing?: boolean
}

/**
 * Where the conversation's compressed context ends. The messages above it are
 * all still here, exactly as they were written; the line only says that the
 * model is sent a summary of them in their place, and opens to show that
 * summary — Markdown, as the model wrote it — with the way to take it back.
 */
export function CompactionDivider({ summary, onUndo, undoing }: CompactionDividerProps) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  const body = useId()

  return (
    <div className="n-compaction" data-open={open || undefined}>
      <button
        type="button"
        className="n-compaction-toggle"
        aria-expanded={open}
        aria-controls={body}
        onClick={() => setOpen((v) => !v)}
      >
        <span className="n-compaction-rule" aria-hidden="true" />
        <span className="n-compaction-label">{t('chat.compaction.divider')}</span>
        <span className="n-compaction-rule" aria-hidden="true" />
      </button>
      {open && (
        <div id={body} className="n-compaction-body" role="region" aria-label={t('chat.compaction.summary-label')}>
          <Markdown text={summary.content} />
          {onUndo && (
            <div className="n-compaction-actions">
              <button type="button" className="n-edit-btn" disabled={undoing} onClick={onUndo}>
                {t('chat.compaction.undo')}
              </button>
            </div>
          )}
        </div>
      )}
    </div>
  )
}

export default CompactionDivider
