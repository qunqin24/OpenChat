import { useTranslation } from 'react-i18next'

import { cn } from '@/lib/utils'
import { CatalogRefreshButton } from './catalog-refresh-button'
import { formatSyncedAt } from '../format'
import { useCatalogStatus, type CatalogStatus } from '../use-catalog'

/**
 * Where the cards' names, limits and prices come from (the models.dev
 * snapshot), in one short line beside the page's action: when it was last
 * fetched, or what is wrong with it. The server refreshes it by itself once it
 * is a day old; the button fetches it now. The detail (a refresh's error, the
 * daily refresh) is in the tooltip, not on the page.
 *
 * A failed refresh outranks the rest: the time beside it would describe the
 * snapshot that refresh failed to replace.
 */
function freshness(status: CatalogStatus, locale: string) {
  if (status.lastError) return { key: 'admin.models.sync-failed', tone: 'error' } as const
  if (!status.fetchedAt) return { key: 'admin.models.never-synced', tone: 'warn' } as const
  if (status.stale) return { key: 'admin.models.stale', tone: 'warn' } as const
  return { key: 'admin.models.synced', tone: 'muted', time: formatSyncedAt(status.fetchedAt, locale) } as const
}

export function CatalogStatusLine() {
  const { t, i18n } = useTranslation()
  const { data: status } = useCatalogStatus()
  const state = status && freshness(status, i18n.language)

  return (
    <div className="flex items-center gap-3">
      {state && (
        <span
          className={cn(
            'text-xs',
            state.tone === 'error' && 'text-destructive',
            state.tone === 'warn' && 'text-amber-600 dark:text-amber-500',
            state.tone === 'muted' && 'text-muted-foreground',
          )}
          title={status?.lastError ?? t('admin.models.sync-hint')}
        >
          {t(state.key, { time: 'time' in state ? state.time : '' })}
        </span>
      )}
      <CatalogRefreshButton />
    </div>
  )
}
