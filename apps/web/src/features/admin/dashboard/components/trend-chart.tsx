import type { AdminDashboard, DashboardRange } from '@openchat/api-client'
import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Bar, BarChart, CartesianGrid, ResponsiveContainer, Tooltip, XAxis, YAxis } from 'recharts'

import { Tabs, TabsList, TabsTrigger } from '@/components/shadcn/ui/tabs'
import { formatCompact, formatCount, formatUsd } from '@/features/admin/usage-logs/format'

type Metric = 'requests' | 'cost'

/**
 * `at` is the start of the bucket in the server's own time zone and says so in
 * its offset. Read from the string, not through `Date`: the label should be the
 * bucket's day, not the day it falls on where the browser happens to be.
 */
const dayOf = (at: string) => at.slice(5, 10)
const hourOf = (at: string) => at.slice(11, 16)

/**
 * Requests per bucket, the failed ones in red on top, or the money spent: one
 * bar per hour in the 24-hour view and per day otherwise, the empty ones
 * included so a quiet stretch looks quiet.
 */
export function TrendChart({
  trend,
  bucket,
  range,
}: {
  trend: AdminDashboard['trend']
  bucket: AdminDashboard['bucket']
  range: DashboardRange
}) {
  const { t } = useTranslation()
  const [metric, setMetric] = useState<Metric>('requests')

  const data = useMemo(
    () => trend.map((point) => ({ ...point, succeeded: point.requests - point.errors })),
    [trend],
  )
  const empty = trend.every((point) => point.requests === 0)
  const label = (at: string) => (bucket === 'hour' ? `${dayOf(at)} ${hourOf(at)}` : `${at.slice(0, 10)}`)

  return (
    <div className="space-y-3">
      <Tabs value={metric} onValueChange={(value) => setMetric(value as Metric)}>
        <TabsList className="h-8">
          <TabsTrigger value="requests" className="px-3 text-xs">
            {t('admin.dashboard.trend.requests')}
          </TabsTrigger>
          <TabsTrigger value="cost" className="px-3 text-xs">
            {t('admin.dashboard.trend.cost')}
          </TabsTrigger>
        </TabsList>
      </Tabs>
      {empty ? (
        <div className="text-muted-foreground flex h-64 items-center justify-center text-sm">
          {t('admin.dashboard.empty')}
        </div>
      ) : (
        <ResponsiveContainer width="100%" height={256} key={range}>
          <BarChart data={data} margin={{ top: 4, right: 4, bottom: 0, left: 0 }}>
            <CartesianGrid vertical={false} stroke="var(--border)" strokeDasharray="3 3" />
            <XAxis
              dataKey="at"
              stroke="#888888"
              fontSize={12}
              tickLine={false}
              axisLine={false}
              minTickGap={24}
              tickFormatter={(at: string) => (bucket === 'hour' ? hourOf(at) : dayOf(at))}
            />
            <YAxis
              stroke="#888888"
              fontSize={12}
              tickLine={false}
              axisLine={false}
              allowDecimals={metric === 'cost'}
              width={metric === 'cost' ? 52 : 40}
              tickFormatter={(value: number) => (metric === 'cost' ? formatUsd(value) : formatCompact(value))}
            />
            <Tooltip
              cursor={{ fill: 'var(--muted)', opacity: 0.5 }}
              contentStyle={{
                background: 'var(--popover)',
                border: '1px solid var(--border)',
                borderRadius: 'var(--radius)',
                color: 'var(--popover-foreground)',
                fontSize: 12,
              }}
              labelStyle={{ color: 'var(--muted-foreground)' }}
              labelFormatter={(at) => label(String(at))}
              formatter={(value, name) => [
                metric === 'cost' ? formatUsd(Number(value)) : formatCount(Number(value)),
                name,
              ]}
            />
            {metric === 'requests' ? (
              <>
                <Bar
                  dataKey="succeeded"
                  name={t('admin.dashboard.trend.success')}
                  stackId="requests"
                  fill="var(--primary)"
                  radius={[0, 0, 0, 0]}
                />
                <Bar
                  dataKey="errors"
                  name={t('admin.dashboard.trend.errors')}
                  stackId="requests"
                  fill="var(--destructive)"
                  radius={[3, 3, 0, 0]}
                />
              </>
            ) : (
              <Bar dataKey="cost" name={t('admin.dashboard.trend.cost')} fill="var(--primary)" radius={[3, 3, 0, 0]} />
            )}
          </BarChart>
        </ResponsiveContainer>
      )}
    </div>
  )
}
