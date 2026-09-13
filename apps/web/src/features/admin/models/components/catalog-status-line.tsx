import { AlertTriangle, Clock } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { cn } from '@/lib/utils'
import { CatalogRefreshButton } from './catalog-refresh-button'
import { formatSyncedAt } from '../format'
import { useCatalogStatus, type CatalogStatus } from '../use-catalog'

/**
 * Four states, in the order of how much they want saying: a refresh that
 * failed, a catalogue never fetched, one that is old, one that is current. The
 * failure outranks the rest because the counts beside it then describe the
 * snapshot the refresh failed to replace, not what upstream has.
 */
function freshness(status: CatalogStatus | undefined, locale: string) {
  if (!status) return undefined
  if (status.lastError) {
    return { key: 'admin.models.sync-failed', tone: 'error', icon: AlertTriangle } as const
  }
  if (!status.fetchedAt) {
    return { key: 'admin.models.never-synced', tone: 'muted', icon: Clock } as const
  }
  if (status.stale) return { key: 'admin.models.stale', tone: 'warn', icon: Clock } as const
  return {
    key: 'admin.models.synced',
    tone: 'muted',
    icon: Clock,
    time: formatSyncedAt(status.fetchedAt, locale),
  } as const
}

export function CatalogStatusLine({ className }: { className?: string }) {
  const { t, i18n } = useTranslation()
  const { data: status } = useCatalogStatus()

  const state = freshness(status, i18n.language)

  return (
    <div className={cn('flex flex-wrap items-center gap-x-3 gap-y-2', className)}>
      {status && (
        <p className="text-muted-foreground text-xs tabular-nums">
          {t('admin.models.count', {
            models: status.modelCount,
            providers: status.providerCount,
          })}
        </p>
      )}

      {state && (
        <p
          className={cn(
            'flex items-center gap-1.5 text-xs',
            state.tone === 'error' && 'text-destructive',
            state.tone === 'warn' && 'text-amber-600 dark:text-amber-500',
            state.tone === 'muted' && 'text-muted-foreground',
          )}
        >
          <state.icon className="size-3.5 shrink-0" aria-hidden="true" />
          {t(state.key, { time: 'time' in state ? state.time : '' })}
        </p>
      )}

      <CatalogRefreshButton />
    </div>
  )
}
