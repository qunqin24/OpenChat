import { Link } from '@tanstack/react-router'
import { type ColumnDef } from '@tanstack/react-table'
import type { TFunction } from 'i18next'

import { LongText } from '@/components/shadcn/long-text'
import { Badge } from '@/components/shadcn/ui/badge'
import { Button } from '@/components/shadcn/ui/button'
import { UserAvatar } from '@/components/user-avatar'
import { formatLogTime } from '@/features/admin/usage-logs/format'
import { categorySlug, type ModerationCategory } from '@/features/admin/moderation/moderation-schema'
import type { ModerationEvent } from '@/features/admin/moderation/use-moderation'

const DASH = '—'

/** A category as the log shows it: the name, or the raw id of one this page has no word for. */
export function categoryLabel(t: TFunction<'common'>, category: string): string {
  const key = `admin.moderation.category.${categorySlug(category as ModerationCategory)}`
  return t(key as 'admin.moderation.category.sexual', { defaultValue: category })
}

/**
 * Built from `t` rather than calling useTranslation in each cell, so the column
 * set stays a plain value the table can memoise. `ban` bans a guest address
 * from the row that was logged against it.
 */
export function moderationLogColumns(
  t: TFunction<'common'>,
  ban: { run: (event: ModerationEvent) => void; pending: boolean },
): ColumnDef<ModerationEvent>[] {
  return [
    {
      id: 'time',
      meta: { title: t('admin.moderation.log.time') },
      header: t('admin.moderation.log.time'),
      cell: ({ row }) => (
        <div className="flex flex-col items-start gap-1">
          <span className="font-mono text-xs whitespace-nowrap">{formatLogTime(row.original.createdAt)}</span>
          <Badge variant="outline" className="px-1.5 py-0 text-[11px]">
            {t(`admin.moderation.source.${row.original.source}`)}
          </Badge>
        </div>
      ),
      enableHiding: false,
    },
    {
      id: 'who',
      meta: { title: t('admin.moderation.log.who') },
      header: t('admin.moderation.log.who'),
      cell: ({ row }) => {
        const event = row.original
        if (!event.userId) {
          return (
            <div className="flex flex-col gap-0.5">
              <span className="text-sm">{t('admin.usage.guest')}</span>
              <span className="text-muted-foreground font-mono text-xs break-all">{event.guestIp ?? DASH}</span>
            </div>
          )
        }
        return (
          <div className="flex items-center gap-2">
            <UserAvatar
              id={event.userId}
              name={event.userName ?? undefined}
              email={event.userEmail ?? undefined}
              className="size-6 text-xs"
            />
            <div className="flex min-w-0 flex-col">
              <LongText className="max-w-36">{event.userName || event.userEmail}</LongText>
              {event.userDisabled && (
                <span className="text-destructive text-xs">{t('admin.moderation.log.disabled')}</span>
              )}
            </div>
          </div>
        )
      },
      enableHiding: false,
    },
    {
      id: 'categories',
      meta: { title: t('admin.moderation.log.categories') },
      header: t('admin.moderation.log.categories'),
      cell: ({ row }) => (
        <div className="flex max-w-56 flex-wrap gap-1">
          {row.original.categories.map((category) => (
            <Badge key={category} variant="secondary" className="px-1.5 py-0 text-[11px]">
              {categoryLabel(t, category)}
            </Badge>
          ))}
        </div>
      ),
    },
    {
      id: 'excerpt',
      meta: { title: t('admin.moderation.log.excerpt') },
      header: t('admin.moderation.log.excerpt'),
      cell: ({ row }) => <LongText className="max-w-80 text-sm">{row.original.excerpt || DASH}</LongText>,
    },
    {
      id: 'sanction',
      meta: { title: t('admin.moderation.log.sanction') },
      header: t('admin.moderation.log.sanction'),
      cell: ({ row }) => {
        const { sanction } = row.original
        return sanction ? (
          <Badge variant="destructive" className="px-1.5 py-0 text-[11px] whitespace-nowrap">
            {t(`admin.moderation.sanction.${sanction}`)}
          </Badge>
        ) : (
          <span className="text-muted-foreground">{DASH}</span>
        )
      },
    },
    {
      id: 'actions',
      meta: { title: t('admin.moderation.log.actions') },
      header: t('admin.moderation.log.actions'),
      cell: ({ row }) => {
        const event = row.original
        if (event.userId) {
          return (
            <Button asChild variant="ghost" size="sm" className="h-7 px-2">
              <Link to="/admin/users">{t('admin.moderation.log.open-account')}</Link>
            </Button>
          )
        }
        if (event.ipBanned) {
          return (
            <Badge variant="outline" className="px-1.5 py-0 text-[11px] whitespace-nowrap">
              {t('admin.moderation.log.banned')}
            </Badge>
          )
        }
        return (
          <Button variant="outline" size="sm" className="h-7 px-2" disabled={ban.pending} onClick={() => ban.run(event)}>
            {t('admin.moderation.log.ban')}
          </Button>
        )
      },
      enableHiding: false,
    },
  ]
}
