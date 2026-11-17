import type { ModelHealth } from '@openchat/api-client'
import { useTranslation } from 'react-i18next'
import { Line, LineChart, ResponsiveContainer, Tooltip, YAxis } from 'recharts'

import { ModelIcon } from '@/components/model-icon'
import { Card } from '@/components/shadcn/ui/card'
import { Skeleton } from '@/components/shadcn/ui/skeleton'
import { formatCompact, formatDuration, formatUsd } from '@/features/admin/usage-logs/format'
import { cn } from '@/lib/utils'
import { useModelHealth } from '../../use-admin'

/**
 * The marketplace's recent tab (design B): a card a model on offer, four to a
 * row, those with requests first (busiest first), each with
 * its speed, time to the first token and success rate over the last six hours
 * as three large figures, and under them how speed and first-token time went
 * in 15-minute steps - so a model that has slowed down or started failing is
 * seen at a glance. Refreshed every minute while the tab is open.
 */
export function ModelHealthGrid({ search }: { search: string }) {
  const { t } = useTranslation()
  const { data, isPending, isError } = useModelHealth()
  // On the page, not the server: the report is every model already.
  const needle = search.trim().toLowerCase()
  const shown = data?.models.filter(
    (m) => !needle || [m.name, m.modelId, m.family].some((field) => field.toLowerCase().includes(needle)),
  )
  if (isPending) {
    return (
      <div className="grid grid-cols-1 gap-4 @xl/content:grid-cols-2 @4xl/content:grid-cols-4">
        {Array.from({ length: 4 }, (_, i) => (
          <Skeleton key={i} className="h-56 rounded-xl" />
        ))}
      </div>
    )
  }
  if (isError || !data) return <p className="text-muted-foreground py-16 text-center text-sm">{t('admin.models.health.failed')}</p>
  if (data.models.length === 0) return <p className="text-muted-foreground py-16 text-center text-sm">{t('admin.models.health.empty')}</p>
  if (!shown?.length) return <p className="text-muted-foreground py-16 text-center text-sm">{t('admin.models.none')}</p>
  return (
    <div className="grid grid-cols-1 gap-4 @xl/content:grid-cols-2 @4xl/content:grid-cols-4">
      {shown.map((model) => (
        <HealthCard key={model.modelId} model={model} buckets={data.buckets} />
      ))}
    </div>
  )
}

type Status = 'ok' | 'some-errors' | 'failing' | 'idle'

/** By the share of requests that failed: any is worth a look, one in twenty is a fault. */
function statusOf(model: ModelHealth): Status {
  if (model.requests === 0) return 'idle'
  if (model.errors === 0) return 'ok'
  return model.errors / model.requests >= 0.05 ? 'failing' : 'some-errors'
}

const statusDot: Record<Status, string> = {
  ok: 'bg-emerald-500',
  'some-errors': 'bg-amber-500',
  failing: 'bg-destructive',
  idle: 'bg-muted-foreground/40',
}

function HealthCard({ model, buckets }: { model: ModelHealth; buckets: string[] }) {
  const { t } = useTranslation()
  const status = statusOf(model)
  const success = model.requests > 0 ? ((model.requests - model.errors) / model.requests) * 100 : null
  const points = model.series.map((point, i) => ({
    at: buckets[i],
    speed: point.speedP50 ?? null,
    firstToken: point.firstTokenP50Ms ?? null,
  }))
  // A line needs two points: with one measured bucket, that bucket is a dot.
  const alone = (key: 'speed' | 'firstToken') => points.filter((p) => p[key] != null).length === 1
  return (
    <Card className="gap-0 px-4 py-3.5">
      <div className="mb-3 flex items-center justify-between gap-2">
        <div className="flex min-w-0 items-center gap-2">
          <ModelIcon subject="model" className="size-5 shrink-0" providerId={model.providerId || undefined} family={model.family || undefined} name={model.name} />
          <span className="truncate font-medium" title={model.modelId}>
            {model.name}
          </span>
        </div>
        <span className="bg-muted text-muted-foreground inline-flex shrink-0 items-center gap-1.5 rounded-full px-2 py-0.5 text-xs">
          <span className={cn('size-1.5 rounded-full', statusDot[status])} aria-hidden="true" />
          {t(`admin.models.health.${status}`)}
        </span>
      </div>

      <dl className="mb-2 grid grid-cols-3 gap-2">
        <Figure label={t('admin.models.health.speed')} value={model.speedP50 == null ? '—' : String(Math.round(model.speedP50))} unit="tok/s" />
        <Figure label={t('admin.models.health.first-token')} value={model.firstTokenP50Ms == null ? '—' : formatDuration(model.firstTokenP50Ms)} />
        <Figure label={t('admin.models.health.success')} value={success == null ? '—' : trimPercent(success)} unit="%" />
      </dl>

      <div className="h-14">
        <ResponsiveContainer width="100%" height="100%">
          {/* Not focusable: Recharts 3 makes a chart a tab stop by default, and a
              click left a focus ring round it. Hovering still shows the figures. */}
          <LineChart data={points} margin={{ top: 4, right: 2, bottom: 2, left: 2 }} accessibilityLayer={false}>
            <YAxis yAxisId="speed" hide domain={[0, 'auto']} />
            <YAxis yAxisId="firstToken" hide domain={[0, 'auto']} />
            <Tooltip
              cursor={{ stroke: 'var(--border)' }}
              contentStyle={{
                background: 'var(--popover)',
                border: '1px solid var(--border)',
                borderRadius: 'var(--radius)',
                color: 'var(--popover-foreground)',
                fontSize: 12,
              }}
              labelStyle={{ color: 'var(--muted-foreground)' }}
              labelFormatter={(_, payload) => clock(String(payload?.[0]?.payload?.at ?? ''))}
              formatter={(value, name) =>
                name === 'speed'
                  ? [`${Math.round(Number(value))} tok/s`, t('admin.models.health.speed')]
                  : [formatDuration(Number(value)), t('admin.models.health.first-token')]
              }
            />
            <Line
              yAxisId="speed"
              dataKey="speed"
              stroke="var(--primary)"
              strokeWidth={1.8}
              dot={alone('speed') ? { r: 2.5, strokeWidth: 0, fill: 'var(--primary)' } : false}
              connectNulls
              isAnimationActive={false}
            />
            <Line
              yAxisId="firstToken"
              dataKey="firstToken"
              stroke="var(--muted-foreground)"
              strokeOpacity={0.6}
              strokeWidth={1.5}
              dot={alone('firstToken') ? { r: 2.5, strokeWidth: 0, fill: 'var(--muted-foreground)' } : false}
              connectNulls
              isAnimationActive={false}
            />
          </LineChart>
        </ResponsiveContainer>
      </div>
      <div className="text-muted-foreground mt-0.5 flex items-center justify-between text-[11px]">
        <span>{t('admin.models.health.from')}</span>
        <span className="flex items-center gap-3">
          <span className="flex items-center gap-1">
            <span className="bg-primary h-0.5 w-2.5 rounded" aria-hidden="true" />
            {t('admin.models.health.speed')}
          </span>
          <span className="flex items-center gap-1">
            <span className="bg-muted-foreground/60 h-0.5 w-2.5 rounded" aria-hidden="true" />
            {t('admin.models.health.first-token')}
          </span>
        </span>
        <span>{t('admin.models.health.now')}</span>
      </div>

      <p className="text-muted-foreground mt-2 text-xs tabular-nums">
        {t('admin.models.health.requests', { count: model.requests })} ·{' '}
        {t('admin.models.health.tokens', { formatted: formatCompact(model.inputTokens + model.outputTokens) })} ·{' '}
        {formatUsd(model.cost)}
      </p>
    </Card>
  )
}

function Figure({ label, value, unit }: { label: string; value: string; unit?: string }) {
  return (
    <div className="min-w-0">
      <dt className="text-muted-foreground text-xs">{label}</dt>
      <dd className="truncate text-lg font-semibold tracking-tight tabular-nums">
        {value}
        {unit && value !== '—' && <span className="text-muted-foreground ms-0.5 text-xs font-normal">{unit}</span>}
      </dd>
    </div>
  )
}

/** 99.6, 100: one decimal where it says something. */
function trimPercent(value: number): string {
  return value >= 99.95 ? '100' : value.toFixed(1)
}

function clock(iso: string): string {
  const date = new Date(iso)
  return Number.isNaN(date.getTime()) ? '' : date.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
}
