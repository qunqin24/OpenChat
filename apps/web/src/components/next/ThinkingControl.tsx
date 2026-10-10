import type { ReasoningEffort } from '@openchat/api-client'
import { Brain, ChevronDown } from 'lucide-react'
import { Popover } from 'radix-ui'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { cn } from '@/lib/utils'
import { ThinkingSlider } from './ThinkingSlider'

interface ThinkingControlProps {
  /** The model's own levels, lowest first, from the catalogue. */
  levels: ReasoningEffort[]
  value: ReasoningEffort
  onChange: (effort: ReasoningEffort) => void
  disabled?: boolean
}

/**
 * The composer's thinking control: a button that names the level in use, and
 * opens the slider to change it. The slider stays out of the bar until asked
 * for, which keeps the bar quiet and leaves the level readable at a glance.
 */
export function ThinkingControl({ levels, value: stored, onChange, disabled }: ThinkingControlProps) {
  const { t } = useTranslation()
  // A level remembered from another model that this one does not have is not
  // sent (the server drops it too), so it reads as automatic.
  const value: ReasoningEffort = levels.includes(stored) ? stored : 'auto'
  // While the knob is dragged, the heading and the button name the level
  // under it, not the one last committed.
  const [preview, setPreview] = useState<ReasoningEffort | null>(null)
  const shown = preview ?? value
  const label = t(`chat.thinking.${shown}`)

  return (
    <Popover.Root>
      <Popover.Trigger asChild disabled={disabled}>
        <button
          type="button"
          className={cn('n-model-trigger', shown !== 'auto' && 'is-active')}
          aria-label={t('chat.thinking.current', { level: label })}
        >
          <Brain aria-hidden="true" />
          <span>{label}</span>
          <ChevronDown aria-hidden="true" />
        </button>
      </Popover.Trigger>
      <Popover.Portal>
        <Popover.Content
          side="top"
          align="end"
          sideOffset={6}
          collisionPadding={8}
          className="n-menu n-think-pop"
          // Opened by a click, focus stays put: moving it to the slider would
          // light its focus ring every time. Tab reaches it from the keyboard.
          onOpenAutoFocus={(event) => event.preventDefault()}
        >
          <div className="n-think-pop-head">
            <span>{t('chat.thinking.heading')}</span>
            <span>{label}</span>
          </div>
          <ThinkingSlider
            levels={levels}
            value={value}
            onChange={onChange}
            onPreview={setPreview}
            disabled={disabled}
          />
        </Popover.Content>
      </Popover.Portal>
    </Popover.Root>
  )
}

export default ThinkingControl
