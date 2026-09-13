import { useTranslation } from 'react-i18next'

import { Avatar, AvatarFallback, AvatarImage } from '@/components/shadcn/ui/avatar'
import { Badge } from '@/components/shadcn/ui/badge'
import { Skeleton } from '@/components/shadcn/ui/skeleton'
import { useAdminUsers } from '@/features/admin/use-admin'
import { formatRelative } from '@/lib/grouping'
import { initialsOf } from '@/lib/initials'

/**
 * Newest accounts. Takes the place of upstream's "Recent Sales" panel, which
 * lists five hardcoded customers — the layout is the same, the rows are real.
 * The list endpoint already sorts by creation date, so the first page is it.
 */
export function RecentUsers() {
  const { t } = useTranslation()
  const { data, isPending } = useAdminUsers({ page: 1, search: '', limit: 5 })

  if (isPending) {
    return (
      <div className="space-y-8">
        {Array.from({ length: 5 }).map((_, index) => (
          <div key={index} className="flex items-center gap-4">
            <Skeleton className="size-9 rounded-full" />
            <Skeleton className="h-8 flex-1" />
          </div>
        ))}
      </div>
    )
  }

  return (
    <div className="space-y-8">
      {data?.items.map((user) => (
        <div key={user.id} className="flex items-center gap-4">
          <Avatar className="h-9 w-9">
            <AvatarImage src={user.avatarUrl} alt={user.name} />
            <AvatarFallback>{initialsOf(user.name || user.email)}</AvatarFallback>
          </Avatar>
          <div className="flex flex-1 flex-wrap items-center justify-between gap-2">
            <div className="space-y-1">
              <p className="text-sm leading-none font-medium">{user.name}</p>
              <p className="text-muted-foreground text-sm">{user.email}</p>
            </div>
            <div className="flex items-center gap-2">
              {user.role === 'admin' && <Badge>{t('admin.users.role-admin')}</Badge>}
              <span className="text-muted-foreground text-sm">
                {formatRelative(user.createdAt, t)}
              </span>
            </div>
          </div>
        </div>
      ))}
    </div>
  )
}
