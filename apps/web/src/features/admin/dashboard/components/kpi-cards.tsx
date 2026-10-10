import type { AdminDashboard } from '@openchat/api-client'
import { Activity, Coins, DatabaseZap, DollarSign, Timer, TriangleAlert, type LucideIcon } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { Card, CardContent, CardHeader, CardTitle } from '@/components/shadcn/ui/card'
import { Skeleton } from '@/components/shadcn/ui/skeleton'
import { formatCompact, formatCount, formatDuration, formatUsd } from '@/features/admin/usage-logs/format'
import { cn } from '@/lib/utils'
import { deltaClass, errorRate, formatRate, pointDelta, relativeDelta, type Delta } from '../format'

type Kpi = {
  key: string
  title: string
  icon: LucideIcon
  value: string
  delta?: Delta
  hint?: string
}

/**
 * The headline figures of the window, each against the window before it of the
 * same length. Whether a change is good depends on the figure: more spend is
 * just more, more failures or a slower first token is worse, a better cache
 * hit rate is better.
 */
export function KpiCards({ data, dimmed }: { data?: AdminDashboard; dimmed: boolean }) {
  const { t } = useTranslation()

  const cards: Kpi[] | undefined = data && (() => {
    const { current: c, previous: p } = data
    const rate = errorRate(c.errors, c.requests)
    const prevRate = errorRate(p.errors, p.requests)
    return [
      {
        key: 'cost',
        title: t('admin.dashboard.kpi.cost'),
        icon: DollarSign,
        value: formatUsd(c.cost),
        delta: relativeDelta(c.cost, p.cost, 'none'),
        hint:
          c.cacheSavings != null
            ? t('admin.dashboard.hint.cost-saved', { v: formatUsd(c.cacheSavings) })
            : t('admin.dashboard.hint.cost-none'),
      },
      {
        key: 'requests',
        title: t('admin.dashboard.kpi.requests'),
        icon: Activity,
        value: formatCount(c.requests),
        delta: relativeDelta(c.requests, p.requests, 'none'),
        hint: t('admin.dashboard.hint.requests', { n: formatCount(c.activeUsers), total: formatCount(data.totalUsers) }),
      },
      {
        key: 'tokens',
        title: t('admin.dashboard.kpi.tokens'),
        icon: Coins,
        value: formatCompact(c.inputTokens + c.cacheReadTokens + c.cacheWriteTokens + c.outputTokens),
        delta: relativeDelta(
          c.inputTokens + c.cacheReadTokens + c.cacheWriteTokens + c.outputTokens,
          p.inputTokens + p.cacheReadTokens + p.cacheWriteTokens + p.outputTokens,
          'none',
        ),
        hint: t('admin.dashboard.hint.tokens', {
          input: formatCompact(c.inputTokens + c.cacheReadTokens + c.cacheWriteTokens),
          output: formatCompact(c.outputTokens),
        }),
      },
      {
        key: 'cache',
        title: t('admin.dashboard.kpi.cache'),
        icon: DatabaseZap,
        value: formatRate(c.cacheHitRate),
        delta: pointDelta(c.cacheHitRate ?? undefined, p.cacheHitRate ?? undefined, 'up'),
        hint: t('admin.dashboard.hint.cache', {
          read: formatCompact(c.cacheReadTokens),
          write: formatCompact(c.cacheWriteTokens),
        }),
      },
      {
        key: 'errors',
        title: t('admin.dashboard.kpi.errors'),
        icon: TriangleAlert,
        value: formatRate(rate),
        delta: pointDelta(rate, prevRate, 'down'),
        hint: t('admin.dashboard.hint.errors', { n: formatCount(c.errors) }),
      },
      {
        key: 'latency',
        title: t('admin.dashboard.kpi.latency'),
        icon: Timer,
        value: c.firstTokenP50Ms == null ? '—' : formatDuration(c.firstTokenP50Ms),
        delta:
          c.firstTokenP50Ms != null && p.firstTokenP50Ms != null
            ? relativeDelta(c.firstTokenP50Ms, p.firstTokenP50Ms, 'down')
            : undefined,
        hint: c.firstTokenP95Ms == null ? undefined : t('admin.dashboard.hint.latency', { v: formatDuration(c.firstTokenP95Ms) }),
      },
    ]
  })()

  return (
    <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-6">
      {(cards ?? Array.from({ length: 6 }, () => undefined)).map((card, index) => (
        <Card key={card?.key ?? index} className="gap-3 py-4">
          <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-0">
            <CardTitle className="text-muted-foreground text-sm font-medium">{card?.title ?? <Skeleton className="h-4 w-16" />}</CardTitle>
            {card && <card.icon className="text-muted-foreground size-4" />}
          </CardHeader>
          <CardContent className={cn('space-y-1 transition-opacity', dimmed && 'opacity-60')}>
            {card ? (
              <>
                <div className="text-2xl font-bold tabular-nums">{card.value}</div>
                <p className="text-muted-foreground min-h-4 text-xs">
                  {card.delta && (
                    <span className={cn('font-medium whitespace-nowrap tabular-nums', deltaClass[card.delta.tone])}>
                      {card.delta.text}
                    </span>
                  )}
                  {card.delta && card.hint && ' · '}
                  {card.hint}
                </p>
              </>
            ) : (
              <>
                <Skeleton className="h-8 w-24" />
                <Skeleton className="h-4 w-32" />
              </>
            )}
          </CardContent>
        </Card>
      ))}
    </div>
  )
}
