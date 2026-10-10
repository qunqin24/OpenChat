import type { AdminDashboard } from '@openchat/api-client'
import { useTranslation } from 'react-i18next'

import { UserAvatar } from '@/components/user-avatar'
import { formatCount, formatUsd } from '@/features/admin/usage-logs/format'
import { Empty } from './empty'

/** Who spends the most in the window, with how many requests it took. */
export function TopUsers({ users }: { users: AdminDashboard['users'] }) {
  const { t } = useTranslation()
  if (users.length === 0) return <Empty />

  return (
    <ul className="space-y-4">
      {users.map((u, index) => (
        <li key={u.userId ?? `none-${index}`} className="flex items-center gap-3">
          <UserAvatar id={u.userId ?? undefined} name={u.userName ?? undefined} email={u.userEmail ?? undefined} className="size-8 text-xs" />
          <div className="min-w-0 flex-1">
            <p className="truncate text-sm leading-none font-medium">
              {u.userName ?? u.userEmail ?? t('admin.dashboard.users.anonymous')}
            </p>
            <p className="text-muted-foreground mt-1 truncate text-xs">
              {u.userName && u.userEmail ? u.userEmail : t('admin.dashboard.users.requests', { n: formatCount(u.requests) })}
            </p>
          </div>
          <div className="text-right">
            <p className="text-sm font-medium tabular-nums">{formatUsd(u.cost)}</p>
            <p className="text-muted-foreground text-xs tabular-nums">
              {t('admin.dashboard.users.requests', { n: formatCount(u.requests) })}
            </p>
          </div>
        </li>
      ))}
    </ul>
  )
}
