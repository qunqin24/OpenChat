import type { TrendPoint } from '@openchat/api-client'
import { Bar, BarChart, ResponsiveContainer, Tooltip, XAxis, YAxis } from 'recharts'
import { useTranslation } from 'react-i18next'

/**
 * Message volume across the trend window. Upstream fills this with random
 * monthly totals; the shape and axis styling are kept, the data is real.
 */
export function Overview({ data }: { data: TrendPoint[] }) {
  const { t, i18n } = useTranslation()

  // `day` arrives as a plain calendar date. Passing it to `new Date` would read
  // it as UTC midnight and land on the previous day west of Greenwich, so the
  // parts are given to the local-time constructor instead.
  const formatDay = (day: string): string => {
    const [year, month, date] = day.split('-').map(Number)
    if (!year || !month || !date) return day
    return new Intl.DateTimeFormat(i18n.language, { month: 'long', day: 'numeric' }).format(
      new Date(year, month - 1, date),
    )
  }

  return (
    <ResponsiveContainer width="100%" height={350}>
      <BarChart data={data}>
        <XAxis
          dataKey="day"
          stroke="#888888"
          fontSize={12}
          tickLine={false}
          axisLine={false}
          minTickGap={24}
          // Drop the year: the window is at most 90 days, so it never varies.
          tickFormatter={(value: string) => value.slice(5)}
        />
        <YAxis
          direction="ltr"
          stroke="#888888"
          fontSize={12}
          tickLine={false}
          axisLine={false}
          allowDecimals={false}
          width={40}
        />
        <Tooltip
          cursor={{ fill: 'var(--muted)' }}
          contentStyle={{
            background: 'var(--popover)',
            border: '1px solid var(--border)',
            borderRadius: 'var(--radius)',
            color: 'var(--popover-foreground)',
            fontSize: 12,
          }}
          labelFormatter={(label) => formatDay(String(label))}
          // Without this the series shows as its raw dataKey, "messages".
          formatter={(value) => [value as number, t('admin.trend.messages')]}
        />
        <Bar dataKey="messages" fill="currentColor" radius={[4, 4, 0, 0]} className="fill-primary" />
      </BarChart>
    </ResponsiveContainer>
  )
}
