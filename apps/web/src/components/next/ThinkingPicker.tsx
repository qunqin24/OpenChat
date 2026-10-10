import type { ReasoningEffort } from '@openchat/api-client'
import { Brain, Check, ChevronDown, CircleOff, Sparkles } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { cn } from '@/lib/utils'

/** Rising bars, as many as the model has thinking levels, filled up to this one. */
function Meter({ level, of }: { level: number; of: number }) {
  return (
    <span className="n-effort-meter" aria-hidden="true">
      {Array.from({ length: of }, (_, i) => i + 1).map((bar) => (
        <span
          key={bar}
          className={cn('n-effort-bar', bar <= level && 'is-on')}
          style={{ height: `${4 + Math.round((8 * bar) / of)}px` }}
        />
      ))}
    </span>
  )
}

interface ThinkingPickerProps {
  /** The model's own levels, lowest first, from the catalogue. */
  levels: ReasoningEffort[]
  value: ReasoningEffort
  onChange: (effort: ReasoningEffort) => void
  disabled?: boolean
}

/**
 * How hard the model thinks. Shown only for models the catalogue says reason;
 * `auto` sends nothing and leaves the model at its own default.
 */
export function ThinkingPicker({ levels, value: stored, onChange, disabled }: ThinkingPickerProps) {
  const { t } = useTranslation()
  // A level remembered from another model that this one does not have is not
  // sent (the server drops it too), so it reads as automatic here.
  const value: ReasoningEffort = levels.includes(stored) ? stored : 'auto'
  const label = t(`chat.thinking.${value}`)
  // `none` is off, not a step on the scale.
  const scale: ReasoningEffort[] = levels.filter((level) => level !== 'none')
  const position = (level: ReasoningEffort) => scale.indexOf(level) + 1
  const options: ReasoningEffort[] = ['auto', ...levels]

  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild disabled={disabled}>
        <button
          type="button"
          className={cn('n-model-trigger', value !== 'auto' && 'is-active')}
          disabled={disabled}
          aria-label={t('chat.thinking.current', { level: label })}
          title={t('chat.thinking.current', { level: label })}
        >
          <Brain aria-hidden="true" />
          <span>{t('chat.thinking.title')}</span>
          {value !== 'auto' && value !== 'none' && <Meter level={position(value)} of={scale.length} />}
          <ChevronDown aria-hidden="true" />
        </button>
      </DropdownMenuTrigger>
      <DropdownMenuContent className="n-model-menu n-effort-menu" align="end" side="top">
        <DropdownMenuLabel>
          <span className="n-menu-label-text">{t('chat.thinking.heading')}</span>
        </DropdownMenuLabel>
        {options.map((effort) => (
          <DropdownMenuItem key={effort} onSelect={() => onChange(effort)}>
            <span className="n-effort-icon">
              {effort === 'auto' ? (
                <Sparkles aria-hidden="true" />
              ) : effort === 'none' ? (
                <CircleOff aria-hidden="true" />
              ) : (
                <Meter level={position(effort)} of={scale.length} />
              )}
            </span>
            <span>{t(`chat.thinking.${effort}`)}</span>
            {effort === value && <Check className="n-menu-check" aria-hidden="true" />}
          </DropdownMenuItem>
        ))}
      </DropdownMenuContent>
    </DropdownMenu>
  )
}

export default ThinkingPicker
