import type { User } from '@openchat/api-client'
import type { ColumnDef } from '@tanstack/react-table'
import type { TFunction } from 'i18next'

import { Badge } from '@/components/shadcn/ui/badge'
import { Checkbox } from '@/components/shadcn/ui/checkbox'
import { formatRelative } from '@/lib/grouping'
import { cn } from '@/lib/utils'

/**
 * Built from `t` rather than calling useTranslation inside the cells, so the
 * column set is a plain value the table can memoise.
 */
export function usersColumns(t: TFunction<'common'>, currentUserId?: string): ColumnDef<User>[] {
  return [
    {
      id: 'select',
      header: ({ table }) => (
        <Checkbox
          checked={
            table.getIsAllPageRowsSelected() ||
            (table.getIsSomePageRowsSelected() && 'indeterminate')
          }
          onCheckedChange={(value) => table.toggleAllPageRowsSelected(Boolean(value))}
          aria-label={t('admin.users.select-all')}
          className="translate-y-[2px]"
        />
      ),
      cell: ({ row }) => (
        <Checkbox
          checked={row.getIsSelected()}
          onCheckedChange={(value) => row.toggleSelected(Boolean(value))}
          aria-label={t('admin.users.select-row')}
          className="translate-y-[2px]"
        />
      ),
      enableSorting: false,
      enableHiding: false,
    },
    {
      accessorKey: 'name',
      header: () => t('admin.users.account'),
      cell: ({ row }) => (
        <div className={cn('flex flex-col', row.original.disabled && 'opacity-60')}>
          <span className="font-medium">{row.original.name}</span>
          <span className="text-muted-foreground text-xs">{row.original.email}</span>
        </div>
      ),
    },
    {
      accessorKey: 'role',
      header: () => t('admin.users.role'),
      cell: ({ row }) => (
        <div className="flex items-center gap-1.5">
          <Badge variant={row.original.role === 'admin' ? 'default' : 'secondary'}>
            {t(`admin.users.role-${row.original.role}`)}
          </Badge>
          {row.original.disabled && (
            <Badge variant="destructive">{t('admin.users.disabled')}</Badge>
          )}
          {row.original.id === currentUserId && (
            <Badge variant="outline">{t('admin.users.you')}</Badge>
          )}
        </div>
      ),
    },
    {
      accessorKey: 'lastSeenAt',
      header: () => t('admin.users.last-seen'),
      cell: ({ row }) => (
        <span className="text-muted-foreground text-sm">
          {formatRelative(row.original.lastSeenAt ?? undefined, t) || '—'}
        </span>
      ),
    },
    {
      accessorKey: 'createdAt',
      header: () => t('admin.users.joined'),
      cell: ({ row }) => (
        <span className="text-muted-foreground text-sm">
          {formatRelative(row.original.createdAt, t)}
        </span>
      ),
    },
  ]
}
