import type { User } from '@openchat/api-client'
import {
  flexRender,
  getCoreRowModel,
  useReactTable,
  type RowSelectionState,
} from '@tanstack/react-table'
import { ChevronLeft, ChevronRight, Search, Shield, ShieldOff, UserCheck, UserX } from 'lucide-react'
import { useEffect, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { DataTableBulkActions } from '@/components/shadcn/data-table'
import { Button } from '@/components/shadcn/ui/button'
import { Card } from '@/components/shadcn/ui/card'
import { Input } from '@/components/shadcn/ui/input'
import { Skeleton } from '@/components/shadcn/ui/skeleton'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/shadcn/ui/table'
import { useAdminUsers, useBulkUpdateUsers } from '@/features/admin/use-admin'
import { useSession } from '@/features/auth/use-auth'
import { usersColumns } from './users-columns'

const PAGE_SIZE = 20

export function UsersPage() {
  const { t } = useTranslation()
  const { data: session } = useSession()
  const bulkUpdate = useBulkUpdateUsers()

  const [input, setInput] = useState('')
  const [search, setSearch] = useState('')
  const [page, setPage] = useState(1)
  const [rowSelection, setRowSelection] = useState<RowSelectionState>({})

  // Debounced so typing does not fire a request per keystroke.
  useEffect(() => {
    const timer = window.setTimeout(() => {
      setSearch(input.trim())
      setPage(1)
    }, 300)
    return () => window.clearTimeout(timer)
  }, [input])

  const { data, isPending, isError } = useAdminUsers({ page, search, limit: PAGE_SIZE })

  const rows = useMemo(() => data?.items ?? [], [data])
  const columns = useMemo(() => usersColumns(t, session?.id), [t, session?.id])
  const pages = Math.max(1, Math.ceil((data?.total ?? 0) / PAGE_SIZE))

  const table = useReactTable<User>({
    data: rows,
    columns,
    state: { rowSelection },
    // Selection survives a re-fetch because rows are keyed by account id
    // rather than by their position on the page.
    getRowId: (row) => row.id,
    onRowSelectionChange: setRowSelection,
    getCoreRowModel: getCoreRowModel(),
    // The server owns paging and filtering; the table only renders what it is
    // handed, so it must not try to slice the rows again.
    manualPagination: true,
    manualFiltering: true,
    pageCount: pages,
  })

  const selected = table.getFilteredSelectedRowModel().rows.map((row) => row.original)

  const applyBulk = async (body: { role?: User['role']; disabled?: boolean }) => {
    if (selected.length === 0) return

    const result = await bulkUpdate.mutateAsync({
      userIds: selected.map((user) => user.id),
      ...body,
    })

    table.resetRowSelection()

    const parts = [t('admin.bulk.done', { n: result.updated })]
    if (result.skipped.length > 0) {
      parts.push(t('admin.bulk.skipped', { n: result.skipped.length }))
    }
    toast.success(parts.join(' · '))
  }

  return (
    <div className="flex flex-1 flex-col gap-4 p-4 md:p-6">
      <div>
        <h1 className="text-2xl font-bold tracking-tight">{t('admin.nav.users')}</h1>
        <p className="text-muted-foreground text-sm">
          {t('admin.users.total', { n: data?.total ?? 0 })}
        </p>
      </div>

      <div className="relative max-w-sm">
        <Search className="text-muted-foreground pointer-events-none absolute start-3 top-1/2 size-4 -translate-y-1/2" />
        <Input
          value={input}
          placeholder={t('admin.users.search')}
          aria-label={t('admin.users.search')}
          className="ps-9"
          onChange={(event) => setInput(event.target.value)}
        />
      </div>

      <Card className="overflow-hidden p-0">
        <Table>
          <TableHeader>
            {table.getHeaderGroups().map((group) => (
              <TableRow key={group.id}>
                {group.headers.map((header) => (
                  <TableHead key={header.id}>
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
                <TableCell colSpan={columns.length} className="h-32 text-center">
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
                  className="text-muted-foreground h-32 text-center"
                >
                  {t('admin.users.none')}
                </TableCell>
              </TableRow>
            )}

            {!isError &&
              !isPending &&
              table.getRowModel().rows.map((row) => (
                <TableRow key={row.id} data-state={row.getIsSelected() && 'selected'}>
                  {row.getVisibleCells().map((cell) => (
                    <TableCell key={cell.id}>
                      {flexRender(cell.column.columnDef.cell, cell.getContext())}
                    </TableCell>
                  ))}
                </TableRow>
              ))}
          </TableBody>
        </Table>
      </Card>

      {pages > 1 && (
        <div className="flex items-center justify-end gap-2">
          <span className="text-muted-foreground text-sm">
            {page} / {pages}
          </span>
          <Button
            variant="outline"
            size="icon"
            disabled={page <= 1}
            aria-label={t('admin.users.previous')}
            onClick={() => {
              setPage((value) => Math.max(1, value - 1))
              table.resetRowSelection()
            }}
          >
            <ChevronLeft />
          </Button>
          <Button
            variant="outline"
            size="icon"
            disabled={page >= pages}
            aria-label={t('admin.users.next')}
            onClick={() => {
              setPage((value) => Math.min(pages, value + 1))
              table.resetRowSelection()
            }}
          >
            <ChevronRight />
          </Button>
        </div>
      )}

      <DataTableBulkActions table={table} entityName={t('admin.bulk.entity-user')}>
        <Button
          variant="secondary"
          size="sm"
          disabled={bulkUpdate.isPending}
          onClick={() => void applyBulk({ role: 'admin' })}
        >
          <Shield />
          {t('admin.bulk.promote')}
        </Button>
        <Button
          variant="secondary"
          size="sm"
          disabled={bulkUpdate.isPending}
          onClick={() => void applyBulk({ role: 'user' })}
        >
          <ShieldOff />
          {t('admin.bulk.demote')}
        </Button>
        <Button
          variant="secondary"
          size="sm"
          disabled={bulkUpdate.isPending}
          onClick={() => void applyBulk({ disabled: false })}
        >
          <UserCheck />
          {t('admin.bulk.enable')}
        </Button>
        <Button
          variant="destructive"
          size="sm"
          disabled={bulkUpdate.isPending}
          onClick={() => void applyBulk({ disabled: true })}
        >
          <UserX />
          {t('admin.bulk.disable')}
        </Button>
      </DataTableBulkActions>
    </div>
  )
}

export default UsersPage
