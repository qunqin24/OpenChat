import { ArrowUp, Maximize2, Minimize2, Plus, Square } from 'lucide-react'
import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { isEnter, withCtrl, withShift } from '@/lib/keys'
import { cn } from '@/lib/utils'
import Tooltip, { TooltipGroup } from './Tooltip'

interface ComposerProps {
  value: string
  onValueChange: (value: string) => void
  onSend: () => void
  onStop: () => void
  working: boolean
  disabled?: boolean
  /** Whether the active model accepts file attachments. */
  canUpload?: boolean
}

export function Composer({
  value,
  onValueChange,
  onSend,
  onStop,
  working,
  disabled,
  canUpload = false,
}: ComposerProps) {
  const { t } = useTranslation()
  const ref = useRef<HTMLTextAreaElement>(null)

  const [expanded, setExpanded] = useState(false)
  const [overflowing, setOverflowing] = useState(false)

  // Grow with the content up to the CSS max-height, then scroll.
  useEffect(() => {
    const el = ref.current
    if (!el) return

    el.style.height = 'auto'
    el.style.height = `${el.scrollHeight}px`

    // `clientHeight` is the height after the CSS clamp, so this asks the browser
    // whether the text actually overflows rather than duplicating the max-height
    // as a magic number here. Skipped while expanded, otherwise the toggle would
    // vanish the moment the taller box fits the text.
    if (!expanded) setOverflowing(el.scrollHeight > el.clientHeight)
  }, [value, expanded])

  const canSend = value.trim().length > 0 && !disabled
  const showToggle = expanded || overflowing

  return (
    // One group for the whole composer: hovering along the button row swaps
    // tooltips instantly instead of re-paying the open delay per button.
    <TooltipGroup>
      <div className={cn('n-composer', expanded && 'expanded', showToggle && 'has-toggle')}>
        {showToggle && (
          <Tooltip
            label={expanded ? t('next.composer.collapse') : t('next.composer.expand')}
            side="left"
          >
            <button
              type="button"
              className="n-composer-toggle n-icon-btn sm"
              aria-expanded={expanded}
              onClick={() => setExpanded(!expanded)}
            >
              {expanded ? (
                <Minimize2 className="h-3.5 w-3.5" />
              ) : (
                <Maximize2 className="h-3.5 w-3.5" />
              )}
            </button>
          </Tooltip>
        )}

        <div className="n-composer-field">
          <textarea
            id="input"
            ref={ref}
            className="n-composer-input"
            rows={1}
            value={value}
            disabled={disabled}
            placeholder={t('chat.placeholder')}
            onChange={(event) => onValueChange(event.target.value)}
            onKeyDown={(event) => {
              if (!isEnter(event)) return
              // Bare Enter sends; Ctrl/Cmd or Shift inserts a newline.
              if (withCtrl(event) || withShift(event)) return
              event.preventDefault()
              if (canSend) onSend()
            }}
          />
        </div>

        <div className="n-composer-bar">
          {/* A disabled trigger fires no pointer events, so the tooltip would go
              silent exactly when the reader most needs to know why the button is
              dead — hence the wrapper span picking the hover up for it. */}
          <Tooltip label={canUpload ? t('next.file.upload') : t('next.file.unavailable')}>
            <span className="n-tip-wrap">
              <button
                type="button"
                className="n-icon-btn"
                disabled={!canUpload}
                aria-label={t('next.file.upload')}
              >
                <Plus className="h-[18px] w-[18px]" />
              </button>
            </span>
          </Tooltip>

          <div className="grow" />

          <Tooltip label={working ? t('chat.stop') : t('chat.send')}>
            <button
              type="button"
              className="n-icon-btn primary"
              disabled={!working && !canSend}
              aria-label={working ? t('chat.stop') : t('chat.send')}
              onClick={() => (working ? onStop() : onSend())}
            >
              {working ? (
                <Square className="h-3.5 w-3.5 fill-current" />
              ) : (
                <ArrowUp className="h-[18px] w-[18px]" />
              )}
            </button>
          </Tooltip>
        </div>
      </div>
    </TooltipGroup>
  )
}

export default Composer
