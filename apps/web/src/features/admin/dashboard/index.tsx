import type { DashboardRange } from '@openchat/api-client'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { ConfigDrawer } from '@/components/shadcn/config-drawer'
import { Header } from '@/components/shadcn/layout/header'
import { Main } from '@/components/shadcn/layout/main'
import { ProfileDropdown } from '@/components/shadcn/profile-dropdown'
import { Search } from '@/components/shadcn/search'
import { Card, CardAction, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/shadcn/ui/card'
import { Skeleton } from '@/components/shadcn/ui/skeleton'
import { Tabs, TabsList, TabsTrigger } from '@/components/shadcn/ui/tabs'
import { useAdminDashboard } from '@/features/admin/use-admin'
import { cn } from '@/lib/utils'
import { ChannelsTable } from './components/channels-table'
import { KpiCards } from './components/kpi-cards'
import { ModelsTable } from './components/models-table'
import { AllErrorsLink, RecentErrors } from './components/recent-errors'
import { TopUsers } from './components/top-users'
import { TrendChart } from './components/trend-chart'

const RANGES: DashboardRange[] = ['24h', '7d', '30d', '90d']

/**
 * How the model requests are going: spend, volume, cache, failures and speed
 * over a window, from the usage ledger -- what an operator of a chat service
 * watches, rather than how many accounts exist.
 */
export function Dashboard() {
  const { t } = useTranslation()
  const [range, setRange] = useState<DashboardRange>('7d')
  const { data, isError, isPlaceholderData } = useAdminDashboard(range)

  // Switching the range keeps the old figures up until the new ones arrive;
  // they are dimmed meanwhile so they are not mistaken for the new window's.
  const dimmed = isPlaceholderData
  const body = (node: React.ReactNode, skeleton = 'h-48') =>
    data ? <div className={cn('transition-opacity', dimmed && 'opacity-60')}>{node}</div> : <Skeleton className={cn('mx-6 w-auto', skeleton)} />

  return (
    <>
      <Header>
        <Search className="me-auto" />
        <ConfigDrawer />
        <ProfileDropdown />
      </Header>

      <Main className="flex flex-1 flex-col gap-4 sm:gap-6">
        <div className="flex flex-wrap items-end justify-between gap-3">
          <div>
            <h1 className="text-2xl font-bold tracking-tight">{t('admin.nav.dashboard')}</h1>
            <p className="text-muted-foreground">{t('admin.dashboard.subtitle')}</p>
          </div>
          <div className="flex items-center gap-3">
            <span className="text-muted-foreground hidden text-xs xl:inline">{t('admin.dashboard.vs-prev')}</span>
            <Tabs value={range} onValueChange={(value) => setRange(value as DashboardRange)}>
              <TabsList>
                {RANGES.map((value) => (
                  <TabsTrigger key={value} value={value}>
                    {t(`admin.dashboard.range.${value}`)}
                  </TabsTrigger>
                ))}
              </TabsList>
            </Tabs>
          </div>
        </div>

        {isError && !data ? (
          <p className="text-destructive text-sm">{t('state.error')}</p>
        ) : (
          <>
            <KpiCards data={data} dimmed={dimmed} />

            <div className="grid grid-cols-1 gap-4 lg:grid-cols-3">
              <Card className="lg:col-span-2">
                <CardHeader>
                  <CardTitle>{t('admin.dashboard.trend.title')}</CardTitle>
                </CardHeader>
                <CardContent>
                  {body(data && <TrendChart trend={data.trend} bucket={data.bucket} range={range} />, 'h-72')}
                </CardContent>
              </Card>

              <Card>
                <CardHeader>
                  <CardTitle>{t('admin.dashboard.users.title')}</CardTitle>
                  <CardDescription>{t('admin.dashboard.users.subtitle')}</CardDescription>
                </CardHeader>
                <CardContent>{body(data && <TopUsers users={data.users} />, 'h-72')}</CardContent>
              </Card>
            </div>

            <Card className="gap-4">
              <CardHeader>
                <CardTitle>{t('admin.dashboard.models.title')}</CardTitle>
                <CardDescription>{t('admin.dashboard.models.subtitle')}</CardDescription>
              </CardHeader>
              <CardContent>{body(data && <ModelsTable models={data.models} />)}</CardContent>
            </Card>

            <div className="grid grid-cols-1 gap-4 lg:grid-cols-3">
              <Card className="gap-4 lg:col-span-2">
                <CardHeader>
                  <CardTitle>{t('admin.dashboard.channels.title')}</CardTitle>
                  <CardDescription>{t('admin.dashboard.channels.subtitle')}</CardDescription>
                </CardHeader>
                <CardContent>{body(data && <ChannelsTable channels={data.channels} />)}</CardContent>
              </Card>

              <Card className="gap-4">
                <CardHeader>
                  <CardTitle>{t('admin.dashboard.errors.title')}</CardTitle>
                  <CardAction>
                    <AllErrorsLink />
                  </CardAction>
                </CardHeader>
                <CardContent>{body(data && <RecentErrors errors={data.errors} />)}</CardContent>
              </Card>
            </div>
          </>
        )}
      </Main>
    </>
  )
}

export default Dashboard
