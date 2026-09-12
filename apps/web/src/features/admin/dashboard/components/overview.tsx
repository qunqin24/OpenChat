import type { TrendPoint } from '@openchat/api-client'
import { Bar, BarChart, ResponsiveContainer, Tooltip, XAxis, YAxis } from 'recharts'

/**
 * Message volume across the trend window. Upstream fills this with random
 * monthly totals; the shape and axis styling are kept, the data is real.
 */
export function Overview({ data }: { data: TrendPoint[] }) {
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
        />
        <Bar dataKey="messages" fill="currentColor" radius={[4, 4, 0, 0]} className="fill-primary" />
      </BarChart>
    </ResponsiveContainer>
  )
}
