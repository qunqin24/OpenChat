import { ArrowUp, Maximize2, Minimize2, Plus, Square } from 'lucide-react'
import { useEffect, useRef, useState, type ClipboardEvent, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'

import type { PendingAttachment } from '@/hooks/use-attachments'
import { isEnter, withCtrl, withShift } from '@/lib/keys'
import { cn } from '@/lib/utils'
import AttachmentTray from './AttachmentTray'
import Tooltip, { TooltipGroup } from './Tooltip'

interface ComposerProps {
  value: string
  onValueChange: (value: string) => void
  onSend: () => void
  onStop: () => void
  working: boolean
  disabled?: boolean
  /** The model picker, docked beside the send button. */
  modelSlot?: ReactNode
  /** Switches for this conversation's tools, beside the attach button. */
  toolsSlot?: ReactNode
  /**
   * File attachments. `accept` is the picker's filter and is empty when the
   * model takes no files, which leaves the button off; `hint` says what the
   * button does, or why it does not.
   */
  attach?: {
    accept: string
    hint: string
    items: PendingAttachment[]
    onAdd: (files: File[]) => void
    onRemove: (key: string) => void
    /** Why sending is held back, when a file is still going up or failed. */
    blocked?: string
  }
}

export function Composer({
  value,
  onValueChange,
  onSend,
  onStop,
  working,
  disabled,
  modelSlot,
  toolsSlot,
  attach,
}: ComposerProps) {
  const { t } = useTranslation()
  const ref = useRef<HTMLTextAreaElement>(null)
  const picker = useRef<HTMLInputElement>(null)

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

  const canUpload = !!attach?.accept
  // Not while an answer is being made: Enter must not queue a second message behind it.
  const canSend = value.trim().length > 0 && !disabled && !working && !attach?.blocked

  // Images copied from elsewhere paste in as files.
  const handlePaste = (event: ClipboardEvent<HTMLTextAreaElement>) => {
    const files = Array.from(event.clipboardData.files)
    if (files.length === 0 || !attach) return
    event.preventDefault()
    attach.onAdd(files)
  }
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
              aria-label={expanded ? t('next.composer.collapse') : t('next.composer.expand')}
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

        {attach && <AttachmentTray items={attach.items} onRemove={attach.onRemove} />}

        <div className="n-composer-field">
          <textarea
            id="input"
            ref={ref}
            className="n-composer-input"
            rows={1}
            value={value}
            disabled={disabled}
            aria-label={t('chat.placeholder')}
            placeholder={t('chat.placeholder')}
            onChange={(event) => onValueChange(event.target.value)}
            onPaste={handlePaste}
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
          <Tooltip label={attach?.hint ?? t('next.file.none')}>
            <span className="n-tip-wrap">
              <button
                type="button"
                className="n-icon-btn"
                disabled={!canUpload}
                aria-label={t('next.file.upload')}
                onClick={() => picker.current?.click()}
              >
                <Plus className="h-[18px] w-[18px]" />
              </button>
            </span>
          </Tooltip>
          {attach && (
            <input
              ref={picker}
              type="file"
              multiple
              hidden
              accept={attach.accept}
              onChange={(event) => {
                attach.onAdd(Array.from(event.target.files ?? []))
                // Picking the same file again must still fire a change.
                event.target.value = ''
              }}
            />
          )}

          {toolsSlot}

          <div className="grow" />

          {modelSlot}

          <Tooltip label={working ? t('chat.stop') : (attach?.blocked ?? t('chat.send'))}>
            <span className="n-tip-wrap">
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
            </span>
          </Tooltip>
        </div>
      </div>
    </TooltipGroup>
  )
}

export default Composer
