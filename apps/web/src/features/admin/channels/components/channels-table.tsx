import type { Channel } from '@openchat/api-client'
import { flexRender, getCoreRowModel, useReactTable, type RowSelectionState } from '@tanstack/react-table'
import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Skeleton } from '@/components/shadcn/ui/skeleton'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/shadcn/ui/table'
import { useAdminChannels } from '@/features/admin/use-admin'
import { cn } from '@/lib/utils'
import { useChannelTypes } from '../channel-types'
import { ChannelsBulkActions } from './channels-bulk-actions'
import { channelsColumns } from './channels-columns'

/**
 * The users table runs in manual mode because the server pages and searches it.
 * Channels come back as one short list — an operator has a handful of them —
 * so there is nothing to page or filter, and this is the plain client-side
 * table react-table gives you for free, with row selection for the bulk
 * toolbar.
 */
export function ChannelsTable() {
  const { t } = useTranslation()
  // The query's own array, not a `?? []` default: that default is a fresh
  // array on every render, and as a useMemo dependency it would rebuild the
  // columns each time.
  const types = useChannelTypes().data
  const { data, isPending, isError } = useAdminChannels()

  const rows = useMemo(() => data ?? [], [data])
  const columns = useMemo(() => channelsColumns(t, types ?? []), [t, types])

  const [rowSelection, setRowSelection] = useState<RowSelectionState>({})

  const table = useReactTable<Channel>({
    data: rows,
    columns,
    // Keyed by channel id so React reuses rows across a refetch, and a
    // selection survives one.
    getRowId: (row) => row.id,
    getCoreRowModel: getCoreRowModel(),
    state: { rowSelection },
    onRowSelectionChange: setRowSelection,
    enableRowSelection: true,
  })

  return (
    <div className="flex flex-1 flex-col gap-4">
      <div className="overflow-hidden rounded-md border">
        <Table>
          <TableHeader>
            {table.getHeaderGroups().map((group) => (
              <TableRow key={group.id} className="group/row">
                {group.headers.map((header) => (
                  <TableHead
                    key={header.id}
                    colSpan={header.colSpan}
                    className={cn(
                      (header.column.columnDef.meta as { className?: string } | undefined)
                        ?.className,
                    )}
                  >
                    {header.isPlaceholder
                      ? null
                      : flexRender(header.column.columnDef.header, header.getContext())}
                  </TableHead>
                ))}
              </TableRow>
            ))}
          </TableHeader>

          <TableBody>
            {isError && (
              <TableRow>
                <TableCell colSpan={columns.length} className="h-24 text-center">
                  {t('state.error')}
                </TableCell>
              </TableRow>
            )}

            {!isError &&
              isPending &&
              Array.from({ length: 3 }).map((_, index) => (
                <TableRow key={index}>
                  <TableCell colSpan={columns.length}>
                    <Skeleton className="h-8 w-full" />
                  </TableCell>
                </TableRow>
              ))}

            {!isError && !isPending && rows.length === 0 && (
              <TableRow>
                <TableCell
                  colSpan={columns.length}
                  className="text-muted-foreground h-24 text-center"
                >
                  {t('admin.channels.none')}
                </TableCell>
              </TableRow>
            )}

            {!isError &&
              !isPending &&
              table.getRowModel().rows.map((row) => (
                <TableRow key={row.id} className="group/row" data-state={row.getIsSelected() && 'selected'}>
                  {row.getVisibleCells().map((cell) => (
                    <TableCell
                      key={cell.id}
                      className={cn(
                        (cell.column.columnDef.meta as { className?: string } | undefined)
                          ?.className,
                      )}
                    >
                      {flexRender(cell.column.columnDef.cell, cell.getContext())}
                    </TableCell>
                  ))}
                </TableRow>
              ))}
          </TableBody>
        </Table>
      </div>
      <ChannelsBulkActions table={table} />
    </div>
  )
}
