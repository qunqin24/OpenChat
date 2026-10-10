import {
  flexRender,
  getCoreRowModel,
  useReactTable,
  type PaginationState,
  type VisibilityState,
} from '@tanstack/react-table'
import { useCallback, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { DataTablePagination } from '@/components/shadcn/data-table'
import { Skeleton } from '@/components/shadcn/ui/skeleton'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/shadcn/ui/table'
import { cn } from '@/lib/utils'
import { useUsageLogs, type UsageFilters, type UsageLog } from '../use-usage-logs'
import { usageLogsColumns } from './usage-logs-columns'
import { UsageLogsToolbar, type PickedUser } from './usage-logs-toolbar'
import { UsageSummary } from './usage-summary'

/**
 * Paging and filtering are the server's job, so the table runs in manual mode
 * and renders exactly the page it is handed -- the toolbar is this page's own
 * because its filters are single choices and a date range, not the stock
 * faceted multi-selects. A link can open it already narrowed (`initialFilters`).
 */
export function UsageLogsTable({ initialFilters }: { initialFilters?: UsageFilters }) {
  const { t } = useTranslation()

  const [columnVisibility, setColumnVisibility] = useState<VisibilityState>({})
  const [filters, setFilters] = useState<UsageFilters>(initialFilters ?? {})
  const [pickedUser, setPickedUser] = useState<PickedUser>()
  const [pagination, setPagination] = useState<PaginationState>({ pageIndex: 0, pageSize: 20 })

  const { data, isPending, isError, refetch, isFetching } = useUsageLogs({
    page: pagination.pageIndex + 1,
    limit: pagination.pageSize,
    filters,
  })

  // A changed filter starts again from the first page: the old position may be
  // past the end of the new, shorter list.
  const backToStart = useCallback(
    () => setPagination((current) => (current.pageIndex === 0 ? current : { ...current, pageIndex: 0 })),
    [],
  )
  const change = useCallback(
    (patch: Partial<UsageFilters>) => {
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
  const columns = useMemo(() => usageLogsColumns(t), [t])
  const pageCount = Math.max(1, Math.ceil((data?.total ?? 0) / pagination.pageSize))

  const table = useReactTable<UsageLog>({
    data: rows,
    columns,
    state: { columnVisibility, pagination },
    // Keyed by the record's id so React reuses rows across a refetch.
    getRowId: (row) => row.id,
    onColumnVisibilityChange: setColumnVisibility,
    onPaginationChange: setPagination,
    getCoreRowModel: getCoreRowModel(),
    manualPagination: true,
    manualFiltering: true,
    pageCount,
  })

  return (
    <div className="flex flex-1 flex-col gap-4">
      <UsageSummary summary={data?.summary} />

      <UsageLogsToolbar
        table={table}
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
              Array.from({ length: 6 }).map((_, index) => (
                <TableRow key={index}>
                  <TableCell colSpan={columns.length}>
                    <Skeleton className="h-9 w-full" />
                  </TableCell>
                </TableRow>
              ))}

            {!isError && !isPending && rows.length === 0 && (
              <TableRow>
                <TableCell
                  colSpan={columns.length}
                  className="text-muted-foreground h-24 text-center"
                >
                  {t('admin.usage.none')}
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

      <DataTablePagination table={table} />
    </div>
  )
}
