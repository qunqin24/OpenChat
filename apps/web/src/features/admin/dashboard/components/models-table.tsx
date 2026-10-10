import type { AdminDashboard } from '@openchat/api-client'
import { Link } from '@tanstack/react-router'
import { useTranslation } from 'react-i18next'

import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/shadcn/ui/table'
import {
  firstTokenTone,
  formatCompact,
  formatCount,
  formatDuration,
  formatUsd,
  toneClass,
} from '@/features/admin/usage-logs/format'
import { cn } from '@/lib/utils'
import { errorRate, errorRateClass, formatRate } from '../format'
import { Empty } from './empty'

/**
 * Busiest models: what they cost, how well their prompts are cached, how often
 * they fail and how soon they start to answer. A model name opens the usage
 * log narrowed to it.
 */
export function ModelsTable({ models }: { models: AdminDashboard['models'] }) {
  const { t } = useTranslation()
  if (models.length === 0) return <Empty />

  const num = 'text-right tabular-nums'
  return (
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead>{t('admin.dashboard.models.model')}</TableHead>
          <TableHead className="text-right">{t('admin.dashboard.models.requests')}</TableHead>
          <TableHead className="text-right">{t('admin.dashboard.models.tokens')}</TableHead>
          <TableHead className="text-right">{t('admin.dashboard.models.cost')}</TableHead>
          <TableHead className="text-right">{t('admin.dashboard.models.cache')}</TableHead>
          <TableHead className="text-right">{t('admin.dashboard.models.errors')}</TableHead>
          <TableHead className="text-right">{t('admin.dashboard.models.latency')}</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {models.map((m) => {
          const rate = errorRate(m.errors, m.requests)
          return (
            <TableRow key={m.model}>
              <TableCell className="max-w-64">
                <Link
                  to="/admin/usage-logs"
                  search={{ model: m.model }}
                  className="block truncate font-mono text-xs hover:underline"
                  title={m.model}
                >
                  {m.model}
                </Link>
              </TableCell>
              <TableCell className={num}>{formatCount(m.requests)}</TableCell>
              <TableCell className={cn(num, 'text-muted-foreground')}>
                {formatCompact(m.inputTokens + m.cacheReadTokens)} / {formatCompact(m.outputTokens)}
              </TableCell>
              <TableCell className={num}>{formatUsd(m.cost)}</TableCell>
              <TableCell className={num}>{formatRate(m.cacheHitRate)}</TableCell>
              <TableCell className={cn(num, errorRateClass(rate))}>{formatRate(rate)}</TableCell>
              <TableCell className="text-right">
                {m.firstTokenP50Ms == null ? (
                  <span className="text-muted-foreground">—</span>
                ) : (
                  <span
                    className={cn(
                      'rounded-md px-1.5 py-0.5 text-xs tabular-nums',
                      toneClass[firstTokenTone(m.firstTokenP50Ms)],
                    )}
                  >
                    {formatDuration(m.firstTokenP50Ms)}
                  </span>
                )}
              </TableCell>
            </TableRow>
          )
        })}
      </TableBody>
    </Table>
  )
}
