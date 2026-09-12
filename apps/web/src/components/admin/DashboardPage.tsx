import { MessageSquare, MessagesSquare, UserCheck, Users } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { useAdminStats } from '@/features/admin/use-admin'
import StatCard from './StatCard'
import TrendChart from './TrendChart'

const WINDOWS = [7, 30, 90] as const

export function DashboardPage() {
  const { t } = useTranslation()
  const [days, setDays] = useState<number>(30)
  const { data, isPending, isError } = useAdminStats(days)

  const totals = data?.totals

  return (
    <main className="n-main">
      <div className="n-topbar">
        <div className="truncate font-medium">{t('admin.nav.dashboard')}</div>
      </div>

      <div className="n-scroll">
        <div className="n-admin-page">
          {isError ? (
            <p className="n-admin-empty">{t('state.error')}</p>
          ) : (
            <>
              <div className="n-log-stats">
                <StatCard
                  title={t('admin.stats.users')}
                  icon={<Users />}
                  loading={isPending}
                  value={totals?.totalUsers.toLocaleString()}
                  hint={t('admin.stats.new-7d', { n: totals?.newUsers7d ?? 0 })}
                />
                <StatCard
                  title={t('admin.stats.active')}
                  icon={<UserCheck />}
                  loading={isPending}
                  value={totals?.activeUsers.toLocaleString()}
                  hint={t('admin.stats.disabled', {
                    n: (totals?.totalUsers ?? 0) - (totals?.activeUsers ?? 0),
                  })}
                />
                <StatCard
                  title={t('admin.stats.conversations')}
                  icon={<MessagesSquare />}
                  loading={isPending}
                  value={totals?.totalConversations.toLocaleString()}
                />
                <StatCard
                  title={t('admin.stats.messages')}
                  icon={<MessageSquare />}
                  loading={isPending}
                  value={totals?.totalMessages.toLocaleString()}
                  hint={t('admin.stats.messages-7d', { n: totals?.messages7d ?? 0 })}
                />
              </div>

              <div className="n-admin-section">
                <div className="n-admin-section-head">
                  <h2 className="n-admin-section-title">{t('admin.trend.title')}</h2>
                  <div className="n-admin-windows">
                    {WINDOWS.map((window) => (
                      <button
                        key={window}
                        type="button"
                        className={`n-admin-window${days === window ? ' active' : ''}`}
                        onClick={() => setDays(window)}
                      >
                        {t('admin.trend.days', { n: window })}
                      </button>
                    ))}
                  </div>
                </div>

                <div className="n-admin-charts">
                  <TrendChart
                    points={data?.trend ?? []}
                    series="messages"
                    label={t('admin.stats.messages')}
                  />
                  <TrendChart
                    points={data?.trend ?? []}
                    series="conversations"
                    label={t('admin.stats.conversations')}
                  />
                  <TrendChart
                    points={data?.trend ?? []}
                    series="users"
                    label={t('admin.stats.new-users')}
                  />
                </div>
              </div>
            </>
          )}
        </div>
      </div>
    </main>
  )
}

export default DashboardPage
