import { Boxes, Plug, Search as SearchIcon, type LucideIcon } from 'lucide-react'
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
import { TabsList, TabsTrigger } from '@/components/shadcn/ui/tabs'
import { CatalogStatusLine } from './catalog-status-line'
import { SCOPES, useCatalogProviders, type CatalogScope } from '../use-catalog'

/** Radix's Select has no value for "no selection", so the sentinel is explicit. */
export const ALL_PROVIDERS = 'all'

/**
 * `as const` so the labels stay literal for `t()`, `satisfies` so every scope
 * the contract allows has to be given a tab here before this compiles.
 */
const SCOPE_TABS = {
  channels: { label: 'admin.models.scope-channels', icon: Plug },
  all: { label: 'admin.models.scope-all', icon: Boxes },
} as const satisfies Record<CatalogScope, { label: string; icon: LucideIcon }>

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
      {/* The scope decides what the grid *means*, so it leads the toolbar rather
          than sitting among the filters: the two answers are mutually exclusive
          and both worth naming on screen. The `Tabs` root is the page's, not
          this component's — Radix passes the value down by context, and keeping
          the root up there lets each tab point at a panel that exists. */}
      <TabsList>
        {SCOPES.map((value) => {
          const { label, icon: Icon } = SCOPE_TABS[value]
          return (
            <TabsTrigger key={value} value={value} className="px-3">
              <Icon aria-hidden="true" />
              {t(label)}
            </TabsTrigger>
          )
        })}
      </TabsList>

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
