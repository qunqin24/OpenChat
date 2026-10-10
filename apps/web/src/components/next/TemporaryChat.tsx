import { Ghost, X } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { useChatStore } from '@/stores/chat-store'
import Tooltip, { TooltipGroup } from './Tooltip'

/**
 * The new-chat screen's switch for a temporary chat: kept out of the history,
 * no memories read or written (custom instructions still apply), deleted a day
 * after it is made. Visibly filled while on, so it cannot be left on unseen.
 */
export function TemporaryToggle() {
  const { t } = useTranslation()
  const on = useChatStore((s) => s.temporaryChat)
  const set = useChatStore((s) => s.setTemporaryChat)
  return (
    // The top bar is outside the sidebar's tooltip group.
    <TooltipGroup>
      <Tooltip label={t('next.temporary.label')} hint={t(on ? 'next.temporary.on-hint' : 'next.temporary.off-hint')} side="bottom">
        <button
          type="button"
          className={on ? 'n-temp-toggle is-on' : 'n-temp-toggle'}
          aria-pressed={on}
          aria-label={t('next.temporary.label')}
          onClick={() => set(!on)}
        >
          <Ghost aria-hidden="true" />
          <span>{t('next.temporary.label')}</span>
        </button>
      </Tooltip>
    </TooltipGroup>
  )
}

/**
 * The band across the top of a temporary chat's frame: what it is, and the way
 * out. The whole chat sits inside a dark frame while it lasts, so a temporary
 * chat can never be mistaken for an ordinary one.
 */
export function TemporaryBar({ onExit }: { onExit: () => void }) {
  const { t } = useTranslation()
  return (
    <div className="n-temp-bar">
      <span className="n-temp-bar-label">
        <Ghost aria-hidden="true" />
        {t('next.temporary.label')}
      </span>
      <button type="button" className="n-temp-bar-exit" aria-label={t('next.temporary.exit')} onClick={onExit}>
        <X aria-hidden="true" />
      </button>
    </div>
  )
}

/** What a temporary chat does and does not keep, said once on its empty screen. */
export function TemporaryNote() {
  const { t } = useTranslation()
  return (
    <p className="n-temp-note" role="status">
      {t('next.temporary.note')}
    </p>
  )
}
