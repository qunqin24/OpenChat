import type { Channel } from '@openchat/api-client'
import { type ColumnDef } from '@tanstack/react-table'
import type { TFunction } from 'i18next'

import { LongText } from '@/components/shadcn/long-text'
import { Badge } from '@/components/shadcn/ui/badge'
import { DataTableColumnHeader } from '@/components/shadcn/data-table'
import { cn } from '@/lib/utils'
import { ChannelEnabledSwitch } from './channel-enabled-switch'
import { ChannelsRowActions } from './channels-row-actions'

/**
 * Built from `t` rather than calling useTranslation in each cell, so the column
 * set stays a plain value the table can memoise.
 */
export function channelsColumns(t: TFunction<'common'>): ColumnDef<Channel>[] {
  return [
    {
      accessorKey: 'name',
      meta: { title: t('admin.channels.name') },
      header: ({ column }) => (
        <DataTableColumnHeader column={column} title={t('admin.channels.name')} />
      ),
      cell: ({ row }) => (
        <LongText className={cn('max-w-48 font-medium', !row.original.enabled && 'opacity-60')}>
          {row.original.name}
        </LongText>
      ),
      enableSorting: false,
      enableHiding: false,
    },
    {
      accessorKey: 'providerId',
      meta: { title: t('admin.channels.provider') },
      header: ({ column }) => (
        <DataTableColumnHeader column={column} title={t('admin.channels.provider')} />
      ),
      // An identifier, not a label — shown verbatim so it can be matched
      // against the catalogue.
      cell: ({ row }) => <span className="font-mono text-sm">{row.original.providerId}</span>,
      enableSorting: false,
    },
    {
      accessorKey: 'baseUrl',
      meta: { title: t('admin.channels.base-url') },
      header: ({ column }) => (
        <DataTableColumnHeader column={column} title={t('admin.channels.base-url')} />
      ),
      // Blank means the provider's own endpoint; the dash says "nothing
      // overridden" without claiming to know what that endpoint is.
      cell: ({ row }) => (
        <LongText className="text-muted-foreground max-w-64 text-sm">
          {row.original.baseUrl || '—'}
        </LongText>
      ),
      enableSorting: false,
    },
    {
      id: 'key',
      meta: { title: t('admin.channels.api-key') },
      accessorFn: (row) => (row.hasKey ? 'set' : 'none'),
      header: ({ column }) => (
        <DataTableColumnHeader column={column} title={t('admin.channels.api-key')} />
      ),
      // The preview is the only part of a key that ever comes back, and it is
      // there to tell two credentials apart rather than to be read.
      cell: ({ row }) =>
        row.original.hasKey ? (
          <div className="flex items-center gap-1.5">
            <Badge variant="outline">{t('admin.channels.api-key-set')}</Badge>
            {row.original.keyPreview && (
              <span className="text-muted-foreground font-mono text-xs">
                {row.original.keyPreview}
              </span>
            )}
          </div>
        ) : (
          <Badge variant="secondary">{t('admin.channels.api-key-none')}</Badge>
        ),
      enableSorting: false,
    },
    {
      accessorKey: 'enabled',
      meta: { title: t('admin.channels.enabled') },
      header: ({ column }) => (
        <DataTableColumnHeader column={column} title={t('admin.channels.enabled')} />
      ),
      cell: ({ row }) => <ChannelEnabledSwitch channel={row.original} />,
      enableSorting: false,
    },
    {
      id: 'actions',
      cell: ({ row }) => <ChannelsRowActions channel={row.original} />,
      meta: { className: 'w-12 text-end' },
      enableSorting: false,
      enableHiding: false,
    },
  ]
}
