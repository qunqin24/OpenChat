import { Cross2Icon } from '@radix-ui/react-icons'
import { RefreshCw } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/shadcn/ui/button'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/shadcn/ui/select'
import { MODERATION_CATEGORIES, categorySlug, type ModerationCategory } from '@/features/admin/moderation/moderation-schema'
import type { ModerationFilters } from '@/features/admin/moderation/use-moderation'
import { UserFilter, type PickedUser } from '@/features/admin/usage-logs/components/usage-logs-toolbar'
import { cn } from '@/lib/utils'

/** Radix Select cannot hold an empty value, so "no constraint" gets a name. */
const ANY = '__any__'

/**
 * Narrows the log by account and by category.
 */
export function ModerationLogsToolbar({
  filters,
  pickedUser,
  onChange,
  onPickUser,
  onReset,
  onRefresh,
  refreshing,
}: {
  filters: ModerationFilters
  pickedUser?: PickedUser
  onChange: (patch: Partial<ModerationFilters>) => void
  onPickUser: (user?: PickedUser) => void
  onReset: () => void
  onRefresh: () => void
  refreshing: boolean
}) {
  const { t } = useTranslation()
  const filtered = Object.values(filters).some(Boolean)

  return (
    <div className="flex flex-wrap items-center gap-2">
      <UserFilter picked={pickedUser} onPick={onPickUser} />

      <Select
        value={filters.category ?? ANY}
        onValueChange={(value) => onChange({ category: value === ANY ? undefined : (value as ModerationCategory) })}
      >
        <SelectTrigger size="sm" className="w-48" aria-label={t('admin.moderation.log.category')}>
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          <SelectItem value={ANY}>{t('admin.moderation.log.any-category')}</SelectItem>
          {MODERATION_CATEGORIES.map((category) => (
            <SelectItem key={category} value={category}>
              {t(`admin.moderation.category.${categorySlug(category)}` as 'admin.moderation.category.sexual')}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>

      {filtered && (
        <Button variant="ghost" onClick={onReset} className="h-8 px-2 lg:px-3">
          {t('admin.ui.table.reset')}
          <Cross2Icon className="ms-2 h-4 w-4" />
        </Button>
      )}

      <div className="ms-auto">
        <Button variant="outline" size="sm" className="h-8" onClick={onRefresh} aria-label={t('admin.usage.refresh')}>
          {/* A loading indicator, so it keeps moving under reduced motion. */}
          <RefreshCw className={cn('size-4', refreshing && 'animate-spin')} data-motion="essential" />
          <span className="hidden sm:inline">{t('admin.usage.refresh')}</span>
        </Button>
      </div>
    </div>
  )
}
