import { useTranslation } from 'react-i18next'

import { ModelIcon } from '@/components/model-icon'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/shadcn/ui/select'
import type { ChannelType, ChannelTypeInfo } from '../channel-types'

type ChannelTypeSelectProps = {
  types: ChannelTypeInfo[]
  isPending: boolean
  value: ChannelType
  onChange: (type: ChannelType) => void
}

/**
 * One dropdown, listing vendors.
 *
 * The protocol is deliberately not shown next to each name. It is what the type
 * decides rather than something to decide alongside it, and putting both on the
 * row invites the reading that they can be mixed — which is the confusion this
 * field exists to remove.
 */
export function ChannelTypeSelect({ types, isPending, value, onChange }: ChannelTypeSelectProps) {
  const { t } = useTranslation()

  return (
    <Select value={value} onValueChange={(next) => onChange(next as ChannelType)}>
      <SelectTrigger className="w-full">
        <SelectValue placeholder={isPending ? t('state.loading') : undefined} />
      </SelectTrigger>
      <SelectContent>
        {types.map((type) => (
          <SelectItem key={type.id} value={type.id}>
            <span className="flex items-center gap-2">
              {/* Empty for the catch-all relay entry, which has no brand to
                  draw; ModelIcon falls back to a neutral square. */}
              <ModelIcon providerId={type.iconId} name={type.label} className="size-4" />
              {type.label}
            </span>
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  )
}
