import type { User, UserStatus } from '@openchat/api-client'
import {
  flexRender,
  getCoreRowModel,
  useReactTable,
  type ColumnFiltersState,
  type PaginationState,
  type RowSelectionState,
  type VisibilityState,
} from '@tanstack/react-table'
import { useEffect, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { DataTablePagination, DataTableToolbar } from '@/components/shadcn/data-table'
import { Skeleton } from '@/components/shadcn/ui/skeleton'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/shadcn/ui/table'
import { useAdminUsers } from '@/features/admin/use-admin'
import { useSession } from '@/features/auth/use-auth'
import { cn } from '@/lib/utils'
import { roles, statuses } from '../data/data'
import { UsersBulkActions } from './data-table-bulk-actions'
import { usersColumns } from './users-columns'

/**
 * Upstream slices a full array in the browser. Paging, searching and filtering
 * are the server's job here, so the table runs in manual mode and renders
 * exactly the page it is handed — but the toolbar, pagination and bulk-actions
 * components are the stock ones, driven by the same table instance.
 */
export function UsersTable() {
  const { t } = useTranslation()
  const { data: session } = useSession()

  const [rowSelection, setRowSelection] = useState<RowSelectionState>({})
  const [columnVisibility, setColumnVisibility] = useState<VisibilityState>({})
  const [columnFilters, setColumnFilters] = useState<ColumnFiltersState>([])
  const [pagination, setPagination] = useState<PaginationState>({ pageIndex: 0, pageSize: 10 })

  // The server takes one free-text query; the name column's filter is what
  // feeds it.
  const search = String(columnFilters.find((filter) => filter.id === 'name')?.value ?? '')

  // The status filter is the server's too (its `status` parameter), so that
  // "pending" lists every account waiting, not just those on this page.
  const statusFilter = columnFilters.find((filter) => filter.id === 'status')?.value
  const status = useMemo(() => (Array.isArray(statusFilter) ? (statusFilter as UserStatus[]) : []), [statusFilter])

  const { data, isPending, isError } = useAdminUsers({
    page: pagination.pageIndex + 1,
    search,
    limit: pagination.pageSize,
    status,
  })

  // A narrowed search or filter can leave the cursor past the last page.
  useEffect(() => {
    setPagination((current) => (current.pageIndex === 0 ? current : { ...current, pageIndex: 0 }))
  }, [search, status])

  const rows = useMemo(() => data?.items ?? [], [data])
  const columns = useMemo(() => usersColumns(t, session?.id), [t, session?.id])
  const pageCount = Math.max(1, Math.ceil((data?.total ?? 0) / pagination.pageSize))

  const table = useReactTable<User>({
    data: rows,
    columns,
    state: { rowSelection, columnVisibility, columnFilters, pagination },
    // Keyed by account id so a selection survives a refetch.
    getRowId: (row) => row.id,
    onRowSelectionChange: setRowSelection,
    onColumnVisibilityChange: setColumnVisibility,
    onColumnFiltersChange: setColumnFilters,
    onPaginationChange: setPagination,
    getCoreRowModel: getCoreRowModel(),
    manualPagination: true,
    manualFiltering: true,
    pageCount,
  })

  return (
    <div className="flex flex-1 flex-col gap-4">
      <DataTableToolbar
        table={table}
        searchPlaceholder={t('admin.users.search')}
        searchKey="name"
        filters={[
          {
            columnId: 'role',
            title: t('admin.users.role'),
            options: roles.map((role) => ({
              label: t(`admin.users.role-${role.value}`),
              value: role.value,
              icon: role.icon,
            })),
          },
          {
            columnId: 'status',
            title: t('admin.users.status'),
            options: statuses.map((status) => ({
              label: t(`admin.users.${status.value}`),
              value: status.value,
              icon: status.icon,
            })),
          },
        ]}
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
              Array.from({ length: 5 }).map((_, index) => (
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
                  {t('admin.users.none')}
                </TableCell>
              </TableRow>
            )}

            {!isError &&
              !isPending &&
              table.getRowModel().rows.map((row) => (
                <TableRow
                  key={row.id}
                  data-state={row.getIsSelected() && 'selected'}
                  className="group/row"
                >
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
      <UsersBulkActions table={table} />
    </div>
  )
}
