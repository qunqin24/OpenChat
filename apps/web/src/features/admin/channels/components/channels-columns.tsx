import type { Channel } from '@openchat/api-client'
import { type ColumnDef } from '@tanstack/react-table'
import type { TFunction } from 'i18next'

import { LongText } from '@/components/shadcn/long-text'
import { Badge } from '@/components/shadcn/ui/badge'
import { Checkbox } from '@/components/shadcn/ui/checkbox'
import { ModelIcon } from '@/components/model-icon'
import { DataTableColumnHeader } from '@/components/shadcn/data-table'
import { cn } from '@/lib/utils'
import { formatUsd } from '@/features/admin/usage-logs/format'
import type { ChannelTypeInfo } from '../channel-types'
import { ChannelEnabledSwitch } from './channel-enabled-switch'
import { ChannelsRowActions } from './channels-row-actions'

/**
 * Built from `t` rather than calling useTranslation in each cell, so the column
 * set stays a plain value the table can memoise.
 */
export function channelsColumns(
  t: TFunction<'common'>,
  /** The server's type table, for the vendor's own name and mark. */
  types: ChannelTypeInfo[],
): ColumnDef<Channel>[] {
  const byId = new Map(types.map((type) => [type.id, type]))

  return [
    {
      id: 'select',
      header: ({ table }) => (
        <Checkbox
          checked={table.getIsAllRowsSelected() || (table.getIsSomeRowsSelected() && 'indeterminate')}
          onCheckedChange={(value) => table.toggleAllRowsSelected(Boolean(value))}
          aria-label={t('admin.channels.select-all')}
          className="translate-y-[2px]"
        />
      ),
      cell: ({ row }) => (
        <Checkbox
          checked={row.getIsSelected()}
          onCheckedChange={(value) => row.toggleSelected(Boolean(value))}
          aria-label={t('admin.channels.select-row')}
          className="translate-y-[2px]"
        />
      ),
      meta: { className: 'w-10' },
      enableSorting: false,
      enableHiding: false,
    },
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
      accessorKey: 'type',
      meta: { title: t('admin.channels.type') },
      header: ({ column }) => (
        <DataTableColumnHeader column={column} title={t('admin.channels.type')} />
      ),
      // The vendor's own mark and its own name, which is what an operator
      // recognises. The protocol behind it is not shown: it is a consequence of
      // this choice rather than a second fact about the channel, and a column
      // repeating "OpenAI" beside "OpenRouter" would invite the reading that
      // the two can disagree.
      cell: ({ row }) => (
        <span className="flex items-center gap-2">
          <ModelIcon
            brand={byId.get(row.original.type)?.iconId}
            name={row.original.type}
            className="size-5"
          />
          {/* The id is the fallback, and a reasonable one: it is what the
              operator picked, spelled the way the contract spells it. */}
          <span className="text-sm">{byId.get(row.original.type)?.label ?? row.original.type}</span>
        </span>
      ),
      enableSorting: false,
    },
    {
      accessorKey: 'modelCount',
      meta: { title: t('admin.channels.models'), className: 'text-end' },
      header: ({ column }) => (
        <DataTableColumnHeader column={column} title={t('admin.channels.models')} />
      ),
      // How many models this channel offers — the count of the ones switched on
      // in the panel's list, so a channel with a fetched list nobody has enabled
      // reads as the zero it is.
      cell: ({ row }) => (
        <span
          className={cn(
            'text-sm tabular-nums',
            row.original.modelCount === 0 && 'text-muted-foreground',
          )}
        >
          {row.original.modelCount}
        </span>
      ),
      enableSorting: false,
    },
    {
      // Where a run of a model several channels offer goes: the highest
      // priority first, then by weight among equals (shown only where it
      // matters, as "×3").
      accessorKey: 'priority',
      meta: { title: t('admin.channels.priority'), className: 'text-end' },
      header: ({ column }) => <DataTableColumnHeader column={column} title={t('admin.channels.priority')} />,
      cell: ({ row }) => (
        <span className="text-sm tabular-nums">
          {row.original.priority}
          {row.original.weight !== 1 && (
            <span className="text-muted-foreground" title={t('admin.channels.weight')}>
              {' '}
              ×{row.original.weight}
            </span>
          )}
        </span>
      ),
      enableSorting: false,
    },
    {
      accessorKey: 'spent',
      meta: { title: t('admin.channels.spent'), className: 'text-end' },
      header: ({ column }) => (
        <DataTableColumnHeader column={column} title={t('admin.channels.spent')} />
      ),
      // All time, from the usage log. The column says what the channel has
      // cost so far; the usage log is where to see when and on what.
      cell: ({ row }) => {
        const spent = row.original.spent ?? 0
        return (
          <span
            className={cn('text-sm tabular-nums', spent === 0 && 'text-muted-foreground')}
            title={t('admin.channels.spent-hint')}
          >
            {formatUsd(spent)}
          </span>
        )
      },
      enableSorting: false,
    },
    {
      accessorKey: 'baseUrl',
      meta: { title: t('admin.channels.base-url') },
      header: ({ column }) => (
        <DataTableColumnHeader column={column} title={t('admin.channels.base-url')} />
      ),
      // Blank means the provider's own endpoint, which is shown as the address
      // the channel actually talks to. The dash is left for a type with no
      // endpoint of its own.
      cell: ({ row }) => (
        <LongText className="text-muted-foreground max-w-64 text-sm">
          {row.original.baseUrl || byId.get(row.original.type)?.endpoint || '—'}
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
