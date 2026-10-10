import { useTranslation } from 'react-i18next'

import { Skeleton } from '@/components/shadcn/ui/skeleton'
import { formatCompact, formatCount, formatPercent, formatUsd } from '../format'
import type { UsageSummary as Summary } from '../use-usage-logs'

/**
 * The totals for everything the filters match -- not just the page on screen,
 * which is why they come from the server with it.
 */
export function UsageSummary({ summary }: { summary?: Summary }) {
  const { t } = useTranslation()

  const tiles = [
    {
      label: t('admin.usage.sum-requests'),
      value: summary ? formatCount(summary.requests) : undefined,
      note: summary && summary.errors > 0 ? t('admin.usage.sum-errors', { n: formatCount(summary.errors) }) : undefined,
      noteTone: 'text-destructive',
    },
    {
      label: t('admin.usage.sum-input'),
      value: summary ? formatCount(summary.inputTokens) : undefined,
    },
    {
      label: t('admin.usage.sum-output'),
      value: summary ? formatCount(summary.outputTokens) : undefined,
    },
    {
      label: t('admin.usage.sum-cost'),
      value: summary ? formatUsd(summary.cost) : undefined,
      note: t('admin.usage.sum-cost-note'),
      noteTone: 'text-muted-foreground',
    },
    {
      label: t('admin.usage.sum-cache'),
      value: summary ? (summary.cacheHitRate == null ? '—' : formatPercent(summary.cacheHitRate)) : undefined,
      note: summary
        ? summary.cacheSavings != null
          ? t('admin.usage.sum-cache-saved', {
              read: formatCompact(summary.cacheReadTokens),
              saved: formatUsd(summary.cacheSavings),
            })
          : t('admin.usage.sum-cache-note', {
              read: formatCompact(summary.cacheReadTokens),
              write: formatCompact(summary.cacheWriteTokens),
            })
        : undefined,
      noteTone: 'text-muted-foreground',
    },
  ]

  return (
    <div className="grid grid-cols-2 gap-3 lg:grid-cols-5">
      {tiles.map((tile) => (
        <div key={tile.label} className="bg-card rounded-lg border px-4 py-3">
          <p className="text-muted-foreground text-xs">{tile.label}</p>
          {tile.value === undefined ? (
            <Skeleton className="mt-1.5 h-7 w-24" />
          ) : (
            <p className="mt-0.5 text-2xl font-semibold tabular-nums">{tile.value}</p>
          )}
          <p className={`mt-0.5 h-4 truncate text-xs ${tile.noteTone ?? ''}`}>{tile.note}</p>
        </div>
      ))}
    </div>
  )
}
