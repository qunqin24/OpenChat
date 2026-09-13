import { Search as SearchIcon } from 'lucide-react'
import { useId } from 'react'
import { useTranslation } from 'react-i18next'

import { Input } from '@/components/shadcn/ui/input'
import { Label } from '@/components/shadcn/ui/label'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/shadcn/ui/select'
import { Switch } from '@/components/shadcn/ui/switch'
import { CatalogStatusLine } from './catalog-status-line'
import { useCatalogProviders } from '../use-catalog'

/** Radix's Select has no value for "no selection", so the sentinel is explicit. */
export const ALL_PROVIDERS = 'all'

type ModelsToolbarProps = {
  search: string
  onSearchChange: (value: string) => void
  providerId: string
  onProviderChange: (value: string) => void
  enabledOnly: boolean
  onEnabledOnlyChange: (value: boolean) => void
}

export function ModelsToolbar({
  search,
  onSearchChange,
  providerId,
  onProviderChange,
  enabledOnly,
  onEnabledOnlyChange,
}: ModelsToolbarProps) {
  const { t } = useTranslation()
  const { data: providers } = useCatalogProviders()
  const enabledSwitchId = useId()

  return (
    <div className="flex flex-col gap-3">
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

        <Select
          value={providerId || ALL_PROVIDERS}
          onValueChange={(value) => onProviderChange(value === ALL_PROVIDERS ? '' : value)}
        >
          <SelectTrigger className="h-8 w-full sm:w-56" aria-label={t('admin.models.all-providers')}>
            <SelectValue placeholder={t('admin.models.all-providers')} />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value={ALL_PROVIDERS}>{t('admin.models.all-providers')}</SelectItem>
            {(providers ?? []).map((provider) => (
              <SelectItem key={provider.id} value={provider.id}>
                {provider.name} ({provider.modelCount})
              </SelectItem>
            ))}
          </SelectContent>
        </Select>

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

      <CatalogStatusLine />
    </div>
  )
}
