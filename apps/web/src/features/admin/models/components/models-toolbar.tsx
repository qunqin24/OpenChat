import { Search as SearchIcon } from 'lucide-react'
import { useId } from 'react'
import { useTranslation } from 'react-i18next'

import { Input } from '@/components/shadcn/ui/input'
import { Label } from '@/components/shadcn/ui/label'
import { Switch } from '@/components/shadcn/ui/switch'

type ModelsToolbarProps = {
  search: string
  onSearchChange: (value: string) => void
  enabledOnly: boolean
  onEnabledOnlyChange: (value: boolean) => void
}

export function ModelsToolbar({
  search,
  onSearchChange,
  enabledOnly,
  onEnabledOnlyChange,
}: ModelsToolbarProps) {
  const { t } = useTranslation()
  const enabledSwitchId = useId()

  return (
    <div className="flex flex-wrap items-center gap-2">
      <div className="relative w-full sm:w-64">
        <SearchIcon
          className="text-muted-foreground pointer-events-none absolute start-2.5 top-1/2 size-4 -translate-y-1/2"
          aria-hidden="true"
        />
        <Input
          value={search}
          onChange={(event) => onSearchChange(event.target.value)}
          placeholder={t('admin.models.search')}
          aria-label={t('admin.models.search')}
          className="h-8 ps-8"
        />
      </div>

      <div className="flex items-center gap-2 ps-1">
        <Switch
          id={enabledSwitchId}
          checked={enabledOnly}
          onCheckedChange={onEnabledOnlyChange}
        />
        <Label htmlFor={enabledSwitchId} className="text-sm font-normal">
          {t('admin.models.filter-enabled')}
        </Label>
      </div>
    </div>
  )
}
