import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Button } from '@/components/shadcn/ui/button'
import { Skeleton } from '@/components/shadcn/ui/skeleton'
import { formatLogTime } from '@/features/admin/usage-logs/format'
import { useGuestBans, useLiftBan } from './use-moderation'

/**
 * The guest network addresses banned now, each with when it began, when it ends
 * and the way back in: lifting a ban is one click, and the address can use guest
 * mode again at once. A shared address (a campus, a company) is why a ban is
 * timed by default.
 */
export function GuestBans() {
  const { t } = useTranslation()
  const bans = useGuestBans()
  const lift = useLiftBan()

  return (
    <div className="space-y-3">
      <div className="space-y-1">
        <div className="text-sm font-medium">{t('admin.moderation.bans.title')}</div>
        <p className="text-muted-foreground text-sm">{t('admin.moderation.bans.hint')}</p>
      </div>

      {bans.isPending ? (
        <Skeleton className="h-16 w-full" />
      ) : bans.isError ? (
        <p className="text-muted-foreground text-sm">{t('state.error')}</p>
      ) : bans.data.length === 0 ? (
        <p className="text-muted-foreground rounded-md border border-dashed p-4 text-center text-sm">
          {t('admin.moderation.bans.none')}
        </p>
      ) : (
        <ul className="divide-y rounded-md border">
          {bans.data.map((ban) => (
            <li key={ban.ip} className="flex flex-wrap items-center justify-between gap-3 p-3">
              <div className="min-w-0 space-y-0.5">
                <div className="font-mono text-sm break-all">{ban.ip}</div>
                <div className="text-muted-foreground text-xs">
                  {t('admin.moderation.bans.since', { time: formatLogTime(ban.createdAt) })}
                  {' · '}
                  {ban.expiresAt
                    ? t('admin.moderation.bans.until', { time: formatLogTime(ban.expiresAt) })
                    : t('admin.moderation.bans.permanent')}
                  {ban.reason && ` · ${ban.reason}`}
                </div>
              </div>
              <Button
                variant="outline"
                size="sm"
                disabled={lift.isPending}
                onClick={() =>
                  lift.mutate(ban.ip, {
                    onSuccess: () => toast.success(t('admin.moderation.bans.lifted', { ip: ban.ip })),
                    onError: () => toast.error(t('admin.settings.system.save-failed')),
                  })
                }
              >
                {t('admin.moderation.bans.lift')}
              </Button>
            </li>
          ))}
        </ul>
      )}
    </div>
  )
}
