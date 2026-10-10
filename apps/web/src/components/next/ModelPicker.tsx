import type { ChatModel } from '@openchat/api-client'
import { Check, ChevronDown } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { ModelIcon } from '@/components/model-icon'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'

interface ModelPickerProps {
  models: ChatModel[]
  /** The selected model's id; may name one no channel offers any more. */
  value: string
  onChange: (modelId: string) => void
  disabled?: boolean
}

/** Which model answers: whatever an enabled channel offers. */
export function ModelPicker({ models, value, onChange, disabled }: ModelPickerProps) {
  const { t } = useTranslation()
  const selected = models.find((m) => m.id === value)
  // A remembered model that is no longer offered still shows its id, so the
  // reader can see what changed; the next send will say it is unavailable.
  const label = selected?.name ?? (value || t('chat.model.none'))

  return (
    <DropdownMenu>
      {/* Disabled on the trigger itself, not only on the button: with asChild
          the menu would still open from a disabled child. */}
      <DropdownMenuTrigger asChild disabled={disabled || models.length === 0}>
        <button
          type="button"
          className="n-model-trigger"
          disabled={disabled || models.length === 0}
          aria-label={t('chat.model.pick')}
          title={label}
        >
          {selected && (
            <ModelIcon
              subject="model"
              className="size-4 rounded-sm"
              providerId={selected.providerId || undefined}
              family={selected.family || undefined}
              name={selected.name}
            />
          )}
          <span>{label}</span>
          <ChevronDown aria-hidden="true" />
        </button>
      </DropdownMenuTrigger>
      <DropdownMenuContent className="n-model-menu" align="end" side="top">
        <DropdownMenuLabel>
          <span className="n-menu-label-text">{t('chat.model.pick')}</span>
        </DropdownMenuLabel>
        {models.map((model) => (
          <DropdownMenuItem key={model.id} onSelect={() => onChange(model.id)}>
            <ModelIcon
              subject="model"
              className="size-4 rounded-sm"
              providerId={model.providerId || undefined}
              family={model.family || undefined}
              name={model.name}
            />
            <span title={model.id}>{model.name}</span>
            {model.id === value && <Check className="n-menu-check" aria-hidden="true" />}
          </DropdownMenuItem>
        ))}
      </DropdownMenuContent>
    </DropdownMenu>
  )
}

export default ModelPicker
