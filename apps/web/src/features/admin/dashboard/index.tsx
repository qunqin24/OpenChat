import { MessageSquare, MessagesSquare, UserCheck, Users } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { ConfigDrawer } from '@/components/shadcn/config-drawer'
import { Header } from '@/components/shadcn/layout/header'
import { Main } from '@/components/shadcn/layout/main'
import { ProfileDropdown } from '@/components/shadcn/profile-dropdown'
import { Search } from '@/components/shadcn/search'
import { ThemeSwitch } from '@/components/shadcn/theme-switch'
import {
  Card,
  CardContent,
  CardHeader,
  CardTitle,
} from '@/components/shadcn/ui/card'
import { Skeleton } from '@/components/shadcn/ui/skeleton'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/shadcn/ui/tabs'
import { useAdminStats } from '@/features/admin/use-admin'
import { Overview } from './components/overview'
import { RecentUsers } from './components/recent-users'

const WINDOWS = ['7', '30', '90'] as const

export function Dashboard() {
  const { t } = useTranslation()
  const [days, setDays] = useState<(typeof WINDOWS)[number]>('30')
  const { data, isPending } = useAdminStats(Number(days))

  const totals = data?.totals

  const cards = [
    {
      title: t('admin.stats.users'),
      icon: Users,
      value: totals?.totalUsers,
      hint: t('admin.stats.new-7d', { n: totals?.newUsers7d ?? 0 }),
    },
    {
      title: t('admin.stats.active'),
      icon: UserCheck,
      value: totals?.activeUsers,
      hint: t('admin.stats.disabled', {
        n: (totals?.totalUsers ?? 0) - (totals?.activeUsers ?? 0),
      }),
    },
    {
      title: t('admin.stats.conversations'),
      icon: MessagesSquare,
      value: totals?.totalConversations,
    },
    {
      title: t('admin.stats.messages'),
      icon: MessageSquare,
      value: totals?.totalMessages,
      hint: t('admin.stats.messages-7d', { n: totals?.messages7d ?? 0 }),
    },
  ]

  return (
    <>
      {/* ===== Top Heading ===== */}
      <Header>
        <Search className="me-auto" />
        <ThemeSwitch />
        <ConfigDrawer />
        <ProfileDropdown />
      </Header>

      {/* ===== Main ===== */}
      <Main>
        <div className="mb-2 flex items-center justify-between space-y-2">
          <h1 className="text-2xl font-bold tracking-tight">{t('admin.nav.dashboard')}</h1>
        </div>

        <Tabs
          orientation="vertical"
          value={days}
          onValueChange={(value) => setDays(value as typeof days)}
          className="space-y-4"
        >
          <div className="w-full overflow-x-auto pb-2">
            <TabsList>
              {WINDOWS.map((window) => (
                <TabsTrigger key={window} value={window}>
                  {t('admin.trend.days', { n: window })}
                </TabsTrigger>
              ))}
            </TabsList>
          </div>

          {WINDOWS.map((window) => (
            <TabsContent key={window} value={window} className="space-y-4">
              <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
                {cards.map((card) => (
                  <Card key={card.title}>
                    <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
                      <CardTitle className="text-sm font-medium">{card.title}</CardTitle>
                      <card.icon className="text-muted-foreground h-4 w-4" />
                    </CardHeader>
                    <CardContent>
                      {isPending ? (
                        <Skeleton className="h-8 w-24" />
                      ) : (
                        <div className="text-2xl font-bold">{card.value?.toLocaleString()}</div>
                      )}
                      {card.hint && !isPending && (
                        <p className="text-muted-foreground text-xs">{card.hint}</p>
                      )}
                    </CardContent>
                  </Card>
                ))}
              </div>

              <div className="grid grid-cols-1 gap-4 lg:grid-cols-7">
                <Card className="col-span-1 lg:col-span-4">
                  <CardHeader>
                    <CardTitle>{t('admin.trend.title')}</CardTitle>
                  </CardHeader>
                  <CardContent className="ps-2">
                    <Overview data={data?.trend ?? []} />
                  </CardContent>
                </Card>

                <Card className="col-span-1 lg:col-span-3">
                  <CardHeader>
                    <CardTitle>{t('admin.recent-users.title')}</CardTitle>
                    <p className="text-muted-foreground text-sm">
                      {t('admin.recent-users.subtitle')}
                    </p>
                  </CardHeader>
                  <CardContent>
                    <RecentUsers />
                  </CardContent>
                </Card>
              </div>
            </TabsContent>
          ))}
        </Tabs>
      </Main>
    </>
  )
}

export default Dashboard
