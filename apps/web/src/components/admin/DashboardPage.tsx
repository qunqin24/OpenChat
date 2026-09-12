import { MessageSquare, MessagesSquare, UserCheck, Users } from 'lucide-react'
import { useState, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import { Area, AreaChart, CartesianGrid, ResponsiveContainer, Tooltip, XAxis, YAxis } from 'recharts'

import { Card, CardContent, CardHeader, CardTitle } from '@/components/shadcn/ui/card'
import { Skeleton } from '@/components/shadcn/ui/skeleton'
import { Tabs, TabsList, TabsTrigger } from '@/components/shadcn/ui/tabs'
import { useAdminStats } from '@/features/admin/use-admin'

const WINDOWS = ['7', '30', '90'] as const

function StatCard({
  title,
  icon,
  value,
  hint,
  loading,
}: {
  title: string
  icon: ReactNode
  value?: number
  hint?: string
  loading: boolean
}) {
  return (
    <Card>
      <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
        <CardTitle className="text-sm font-medium">{title}</CardTitle>
        <span className="text-muted-foreground [&_svg]:size-4">{icon}</span>
      </CardHeader>
      <CardContent>
        {loading ? (
          <Skeleton className="h-8 w-24" />
        ) : (
          <div className="text-2xl font-bold tabular-nums">{value?.toLocaleString()}</div>
        )}
        {hint && !loading && <p className="text-muted-foreground mt-1 text-xs">{hint}</p>}
      </CardContent>
    </Card>
  )
}

export function DashboardPage() {
  const { t } = useTranslation()
  const [days, setDays] = useState<(typeof WINDOWS)[number]>('30')
  const { data, isPending, isError } = useAdminStats(Number(days))

  const totals = data?.totals

  return (
    <div className="flex flex-1 flex-col gap-4 p-4 md:p-6">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-bold tracking-tight">{t('admin.nav.dashboard')}</h1>
        <Tabs value={days} onValueChange={(value) => setDays(value as typeof days)}>
          <TabsList>
            {WINDOWS.map((window) => (
              <TabsTrigger key={window} value={window}>
                {t('admin.trend.days', { n: window })}
              </TabsTrigger>
            ))}
          </TabsList>
        </Tabs>
      </div>

      {isError ? (
        <Card>
          <CardContent className="text-muted-foreground py-16 text-center text-sm">
            {t('state.error')}
          </CardContent>
        </Card>
      ) : (
        <>
          <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
            <StatCard
              title={t('admin.stats.users')}
              icon={<Users />}
              loading={isPending}
              value={totals?.totalUsers}
              hint={t('admin.stats.new-7d', { n: totals?.newUsers7d ?? 0 })}
            />
            <StatCard
              title={t('admin.stats.active')}
              icon={<UserCheck />}
              loading={isPending}
              value={totals?.activeUsers}
              hint={t('admin.stats.disabled', {
                n: (totals?.totalUsers ?? 0) - (totals?.activeUsers ?? 0),
              })}
            />
            <StatCard
              title={t('admin.stats.conversations')}
              icon={<MessagesSquare />}
              loading={isPending}
              value={totals?.totalConversations}
            />
            <StatCard
              title={t('admin.stats.messages')}
              icon={<MessageSquare />}
              loading={isPending}
              value={totals?.totalMessages}
              hint={t('admin.stats.messages-7d', { n: totals?.messages7d ?? 0 })}
            />
          </div>

          <Card>
            <CardHeader>
              <CardTitle>{t('admin.trend.title')}</CardTitle>
            </CardHeader>
            <CardContent className="ps-0">
              <ResponsiveContainer width="100%" height={320}>
                <AreaChart data={data?.trend ?? []} margin={{ top: 8, right: 16, bottom: 0 }}>
                  <defs>
                    {/* One gradient per series, faded out so overlapping areas
                        stay readable rather than stacking into a solid block. */}
                    {['messages', 'conversations', 'users'].map((key, index) => (
                      <linearGradient key={key} id={`fill-${key}`} x1="0" y1="0" x2="0" y2="1">
                        <stop
                          offset="5%"
                          stopColor={`var(--chart-${index + 1})`}
                          stopOpacity={0.5}
                        />
                        <stop
                          offset="95%"
                          stopColor={`var(--chart-${index + 1})`}
                          stopOpacity={0.05}
                        />
                      </linearGradient>
                    ))}
                  </defs>
                  <CartesianGrid vertical={false} stroke="var(--border)" />
                  <XAxis
                    dataKey="day"
                    tickLine={false}
                    axisLine={false}
                    tickMargin={8}
                    minTickGap={24}
                    stroke="var(--muted-foreground)"
                    fontSize={12}
                    tickFormatter={(value: string) => value.slice(5)}
                  />
                  <YAxis
                    tickLine={false}
                    axisLine={false}
                    width={40}
                    allowDecimals={false}
                    stroke="var(--muted-foreground)"
                    fontSize={12}
                  />
                  <Tooltip
                    contentStyle={{
                      background: 'var(--popover)',
                      border: '1px solid var(--border)',
                      borderRadius: 'var(--radius)',
                      color: 'var(--popover-foreground)',
                      fontSize: 12,
                    }}
                  />
                  <Area
                    type="monotone"
                    dataKey="messages"
                    name={t('admin.stats.messages')}
                    stroke="var(--chart-1)"
                    fill="url(#fill-messages)"
                  />
                  <Area
                    type="monotone"
                    dataKey="conversations"
                    name={t('admin.stats.conversations')}
                    stroke="var(--chart-2)"
                    fill="url(#fill-conversations)"
                  />
                  <Area
                    type="monotone"
                    dataKey="users"
                    name={t('admin.stats.new-users')}
                    stroke="var(--chart-3)"
                    fill="url(#fill-users)"
                  />
                </AreaChart>
              </ResponsiveContainer>
            </CardContent>
          </Card>
        </>
      )}
    </div>
  )
}

export default DashboardPage
