import type { User } from '@openchat/api-client'
import { type ColumnDef } from '@tanstack/react-table'
import type { TFunction } from 'i18next'

import { cn } from '@/lib/utils'
import { formatRelative } from '@/lib/grouping'
import { LongText } from '@/components/shadcn/long-text'
import { Badge } from '@/components/shadcn/ui/badge'
import { Checkbox } from '@/components/shadcn/ui/checkbox'
import { DataTableColumnHeader } from '@/components/shadcn/data-table'

/**
 * Built from `t` rather than calling useTranslation in each cell, so the column
 * set stays a plain value the table can memoise.
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
      meta: { className: 'w-10' },
      enableSorting: false,
      enableHiding: false,
    },
    {
      accessorKey: 'name',
      header: ({ column }) => (
        <DataTableColumnHeader column={column} title={t('admin.users.account')} />
      ),
      cell: ({ row }) => (
        <div className={cn('flex flex-col', row.original.disabled && 'opacity-60')}>
          <LongText className="max-w-48 font-medium">{row.original.name}</LongText>
          <span className="text-muted-foreground text-xs">{row.original.email}</span>
        </div>
      ),
      enableHiding: false,
    },
    {
      accessorKey: 'role',
      header: ({ column }) => (
        <DataTableColumnHeader column={column} title={t('admin.users.role')} />
      ),
      cell: ({ row }) => (
        <div className="flex items-center gap-1.5">
          <Badge variant={row.original.role === 'admin' ? 'default' : 'secondary'}>
            {t(`admin.users.role-${row.original.role}`)}
          </Badge>
          {row.original.id === currentUserId && (
            <Badge variant="outline">{t('admin.users.you')}</Badge>
          )}
        </div>
      ),
      enableSorting: false,
    },
    {
      id: 'status',
      accessorFn: (row) => (row.disabled ? 'disabled' : 'enabled'),
      header: ({ column }) => (
        <DataTableColumnHeader column={column} title={t('admin.users.status')} />
      ),
      cell: ({ row }) =>
        row.original.disabled ? (
          <Badge variant="destructive">{t('admin.users.disabled')}</Badge>
        ) : (
          <Badge variant="outline">{t('admin.users.enabled')}</Badge>
        ),
      enableSorting: false,
    },
    {
      accessorKey: 'lastSeenAt',
      header: ({ column }) => (
        <DataTableColumnHeader column={column} title={t('admin.users.last-seen')} />
      ),
      cell: ({ row }) => (
        <span className="text-muted-foreground text-sm">
          {formatRelative(row.original.lastSeenAt ?? undefined, t) || '—'}
        </span>
      ),
      enableSorting: false,
    },
    {
      accessorKey: 'createdAt',
      header: ({ column }) => (
        <DataTableColumnHeader column={column} title={t('admin.users.joined')} />
      ),
      cell: ({ row }) => (
        <span className="text-muted-foreground text-sm">
          {formatRelative(row.original.createdAt, t)}
        </span>
      ),
      enableSorting: false,
    },
  ]
}
