import {
  flexRender,
  getCoreRowModel,
  useReactTable,
  type VisibilityState,
} from '@tanstack/react-table'
import { useCallback, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { DataTableCursorPagination } from '@/components/shadcn/data-table'
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
 *
 * The ledger is paged by cursor, which only goes forward: each page says where
 * the next begins. Going back is going to a cursor already seen, so the ones
 * visited are kept (`trail`, the first page having none). A change of filters
 * or of page size makes them meaningless and starts the trail again.
 */
export function UsageLogsTable({ initialFilters }: { initialFilters?: UsageFilters }) {
  const { t } = useTranslation()

  const [columnVisibility, setColumnVisibility] = useState<VisibilityState>({})
  const [filters, setFilters] = useState<UsageFilters>(initialFilters ?? {})
  const [pickedUser, setPickedUser] = useState<PickedUser>()
  const [pageSize, setPageSize] = useState(20)
  const [trail, setTrail] = useState<{ cursors: (string | undefined)[]; index: number }>({
    cursors: [undefined],
    index: 0,
  })

  const { data, isPending, isError, refetch, isFetching } = useUsageLogs({
    cursor: trail.cursors[trail.index],
    limit: pageSize,
    filters,
  })

  // A changed filter starts again from the first page: a cursor is only good
  // for the filters it was issued under.
  const backToStart = useCallback(() => setTrail({ cursors: [undefined], index: 0 }), [])
  const goNext = useCallback(() => {
    const next = data?.nextCursor
    if (!next) return
    setTrail((current) => ({
      cursors: [...current.cursors.slice(0, current.index + 1), next],
      index: current.index + 1,
    }))
  }, [data?.nextCursor])
  const goPrevious = useCallback(
    () => setTrail((current) => ({ ...current, index: Math.max(0, current.index - 1) })),
    [],
  )
  const changePageSize = useCallback(
    (size: number) => {
      setPageSize(size)
      backToStart()
    },
    [backToStart],
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

  const table = useReactTable<UsageLog>({
    data: rows,
    columns,
    state: { columnVisibility },
    // Keyed by the record's id so React reuses rows across a refetch.
    getRowId: (row) => row.id,
    onColumnVisibilityChange: setColumnVisibility,
    getCoreRowModel: getCoreRowModel(),
    manualPagination: true,
    manualFiltering: true,
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
