import { flexRender, getCoreRowModel, useReactTable } from '@tanstack/react-table'
import { useCallback, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { DataTableCursorPagination } from '@/components/shadcn/data-table'
import { Skeleton } from '@/components/shadcn/ui/skeleton'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/shadcn/ui/table'
import {
  useModerationEvents,
  type ModerationEvent,
  type ModerationFilters,
} from '@/features/admin/moderation/use-moderation'
import type { PickedUser } from '@/features/admin/usage-logs/components/usage-logs-toolbar'
import { cn } from '@/lib/utils'
import { moderationLogColumns } from './columns'
import { ModerationLogsToolbar } from './toolbar'

/**
 * The log of blocked messages, paged by cursor like the usage log: a cursor
 * only goes forward, so the ones visited are kept (`trail`) to go back to, and
 * a change of filter or of page size starts the trail again.
 */
export function ModerationLogsTable() {
  const { t } = useTranslation()
  const [filters, setFilters] = useState<ModerationFilters>({})
  const [pickedUser, setPickedUser] = useState<PickedUser>()
  const [pageSize, setPageSize] = useState(20)
  const [trail, setTrail] = useState<{ cursors: (string | undefined)[]; index: number }>({
    cursors: [undefined],
    index: 0,
  })

  const { data, isPending, isError, refetch, isFetching } = useModerationEvents({
    cursor: trail.cursors[trail.index],
    limit: pageSize,
    filters,
  })

  const backToStart = useCallback(() => setTrail({ cursors: [undefined], index: 0 }), [])
  const goNext = useCallback(() => {
    const next = data?.nextCursor
    if (!next) return
    setTrail((current) => ({
      cursors: [...current.cursors.slice(0, current.index + 1), next],
      index: current.index + 1,
    }))
  }, [data?.nextCursor])
  const goPrevious = useCallback(() => setTrail((current) => ({ ...current, index: Math.max(0, current.index - 1) })), [])
  const changePageSize = useCallback(
    (size: number) => {
      setPageSize(size)
      backToStart()
    },
    [backToStart],
  )
  const change = useCallback(
    (patch: Partial<ModerationFilters>) => {
      setFilters((current) => ({ ...current, ...patch }))
      backToStart()
    },
    [backToStart],
  )
  const pickUser = useCallback(
    (user?: PickedUser) => {
      setPickedUser(user)
      change({ userId: user?.id })
    },
    [change],
  )
  const reset = useCallback(() => {
    setFilters({})
    setPickedUser(undefined)
    backToStart()
  }, [backToStart])

  const rows = useMemo(() => data?.items ?? [], [data])
  const columns = useMemo(() => moderationLogColumns(t), [t])

  const table = useReactTable<ModerationEvent>({
    data: rows,
    columns,
    getRowId: (row) => row.id,
    getCoreRowModel: getCoreRowModel(),
    manualPagination: true,
    manualFiltering: true,
  })

  return (
    <div className="flex flex-1 flex-col gap-4">
      <ModerationLogsToolbar
        filters={filters}
        pickedUser={pickedUser}
        onChange={change}
        onPickUser={pickUser}
        onReset={reset}
        onRefresh={() => void refetch()}
        refreshing={isFetching}
      />

      <div className="overflow-hidden rounded-md border">
        <Table>
          <TableHeader>
            {table.getHeaderGroups().map((group) => (
              <TableRow key={group.id} className="group/row">
                {group.headers.map((header) => (
                  <TableHead
                    key={header.id}
                    colSpan={header.colSpan}
                    className={cn((header.column.columnDef.meta as { className?: string } | undefined)?.className)}
                  >
                    {header.isPlaceholder ? null : flexRender(header.column.columnDef.header, header.getContext())}
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
              Array.from({ length: 6 }).map((_, index) => (
                <TableRow key={index}>
                  <TableCell colSpan={columns.length}>
                    <Skeleton className="h-9 w-full" />
                  </TableCell>
                </TableRow>
              ))}

            {!isError && !isPending && rows.length === 0 && (
              <TableRow>
                <TableCell colSpan={columns.length} className="text-muted-foreground h-24 text-center">
                  {t('admin.moderation.log.none')}
                </TableCell>
              </TableRow>
            )}

            {!isError &&
              !isPending &&
              table.getRowModel().rows.map((row) => (
                <TableRow key={row.id} className="group/row">
                  {row.getVisibleCells().map((cell) => (
                    <TableCell
                      key={cell.id}
                      className={cn((cell.column.columnDef.meta as { className?: string } | undefined)?.className)}
                    >
                      {flexRender(cell.column.columnDef.cell, cell.getContext())}
                    </TableCell>
                  ))}
                </TableRow>
              ))}
          </TableBody>
        </Table>
      </div>

      <DataTableCursorPagination
        page={trail.index + 1}
        pageSize={pageSize}
        onPageSizeChange={changePageSize}
        canPrevious={trail.index > 0}
        canNext={Boolean(data?.nextCursor) && !isFetching}
        onFirst={backToStart}
        onPrevious={goPrevious}
        onNext={goNext}
      />
    </div>
  )
}
