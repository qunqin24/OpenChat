import type { components } from '@openchat/api-client'
import { keepPreviousData, useQuery } from '@tanstack/react-query'
import { flexRender, getCoreRowModel, useReactTable, type ColumnDef } from '@tanstack/react-table'
import type { TFunction } from 'i18next'
import { RefreshCw } from 'lucide-react'
import { useCallback, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { ConfigDrawer } from '@/components/shadcn/config-drawer'
import { DataTableCursorPagination } from '@/components/shadcn/data-table'
import { Header } from '@/components/shadcn/layout/header'
import { Main } from '@/components/shadcn/layout/main'
import { LongText } from '@/components/shadcn/long-text'
import { Search } from '@/components/shadcn/search'
import { Badge } from '@/components/shadcn/ui/badge'
import { Button } from '@/components/shadcn/ui/button'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/shadcn/ui/select'
import { Skeleton } from '@/components/shadcn/ui/skeleton'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/shadcn/ui/table'
import { UserAvatar } from '@/components/user-avatar'
import { useAdminPlugins } from '@/features/admin/plugins/use-admin-plugins'
import { adminKeys } from '@/features/admin/use-admin'
import { formatLogTime } from '@/features/admin/usage-logs/format'
import { api, unwrap } from '@/lib/api'
import { cn } from '@/lib/utils'

type PluginCallLog = components['schemas']['PluginCallLog']

interface Filters {
  pluginId?: string
  failed?: boolean
}

const ALL = 'all'
const DASH = '—'

/**
 * Plugin tool calls (api/plugins_calls.go, plugin_calls): who called which
 * tool, how it ended, whether they were asked first and how long it took -
 * never what went in or came out. Paged by cursor as the other logs are: the
 * cursors visited are kept to go back to, and a change of filter starts again.
 */
export function PluginCalls() {
  const { t } = useTranslation()
  const plugins = useAdminPlugins()
  const [filters, setFilters] = useState<Filters>({})
  const [pageSize, setPageSize] = useState(20)
  const [trail, setTrail] = useState<{ cursors: (string | undefined)[]; index: number }>({ cursors: [undefined], index: 0 })
  const cursor = trail.cursors[trail.index]

  const { data, isPending, isError, refetch, isFetching } = useQuery({
    queryKey: adminKeys.pluginCalls({ ...filters, cursor, pageSize }),
    queryFn: async ({ signal }) =>
      unwrap(
        await api.GET('/admin/plugin-calls', {
          params: { query: { cursor, limit: pageSize, pluginId: filters.pluginId, failed: filters.failed || undefined } },
          signal,
        }),
      ),
    placeholderData: keepPreviousData,
  })

  const backToStart = useCallback(() => setTrail({ cursors: [undefined], index: 0 }), [])
  const change = (patch: Filters) => {
    setFilters((current) => ({ ...current, ...patch }))
    backToStart()
  }
  const goNext = () => {
    const next = data?.nextCursor
    if (!next) return
    setTrail((current) => ({ cursors: [...current.cursors.slice(0, current.index + 1), next], index: current.index + 1 }))
  }

  const rows = useMemo(() => data?.items ?? [], [data])
  const columns = useMemo(() => callColumns(t), [t])
  const table = useReactTable<PluginCallLog>({
    data: rows,
    columns,
    getRowId: (row) => String(row.id),
    getCoreRowModel: getCoreRowModel(),
    manualPagination: true,
    manualFiltering: true,
  })

  return (
    <>
      <Header fixed>
        <Search className="me-auto" />
        <ConfigDrawer />
      </Header>

      <Main className="flex flex-1 flex-col gap-4 sm:gap-6">
        <div>
          <h2 className="text-2xl font-bold tracking-tight">{t('admin.nav.plugin-calls')}</h2>
          <p className="text-muted-foreground">{t('admin.plugin-calls.subtitle')}</p>
        </div>

        <div className="flex flex-wrap items-center gap-2">
          <Select value={filters.pluginId ?? ALL} onValueChange={(v) => change({ pluginId: v === ALL ? undefined : v })}>
            <SelectTrigger className="h-8 w-44">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value={ALL}>{t('admin.plugin-calls.all-plugins')}</SelectItem>
              {(plugins.data?.items ?? []).map((p) => (
                <SelectItem key={p.id} value={p.id}>
                  {p.name}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          <Select value={filters.failed ? 'failed' : ALL} onValueChange={(v) => change({ failed: v === 'failed' })}>
            <SelectTrigger className="h-8 w-36">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value={ALL}>{t('admin.plugin-calls.all-calls')}</SelectItem>
              <SelectItem value="failed">{t('admin.plugin-calls.failed-only')}</SelectItem>
            </SelectContent>
          </Select>
          <div className="grow" />
          <Button variant="outline" size="sm" className="h-8" onClick={() => void refetch()} disabled={isFetching}>
            <RefreshCw className={cn(isFetching && 'animate-spin')} aria-hidden="true" />
            {t('admin.plugin-calls.refresh')}
          </Button>
        </div>

        <div className="overflow-hidden rounded-md border">
          <Table>
            <TableHeader>
              {table.getHeaderGroups().map((group) => (
                <TableRow key={group.id}>
                  {group.headers.map((header) => (
                    <TableHead key={header.id}>{header.isPlaceholder ? null : flexRender(header.column.columnDef.header, header.getContext())}</TableHead>
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
                    {t('admin.plugin-calls.none')}
                  </TableCell>
                </TableRow>
              )}
              {!isError &&
                !isPending &&
                table.getRowModel().rows.map((row) => (
                  <TableRow key={row.id}>
                    {row.getVisibleCells().map((cell) => (
                      <TableCell key={cell.id}>{flexRender(cell.column.columnDef.cell, cell.getContext())}</TableCell>
                    ))}
                  </TableRow>
                ))}
            </TableBody>
          </Table>
        </div>

        <DataTableCursorPagination
          page={trail.index + 1}
          pageSize={pageSize}
          onPageSizeChange={(size) => {
            setPageSize(size)
            backToStart()
          }}
          canPrevious={trail.index > 0}
          canNext={Boolean(data?.nextCursor) && !isFetching}
          onFirst={backToStart}
          onPrevious={() => setTrail((current) => ({ ...current, index: Math.max(0, current.index - 1) }))}
          onNext={goNext}
        />
      </Main>
    </>
  )
}

function callColumns(t: TFunction<'common'>): ColumnDef<PluginCallLog>[] {
  return [
    {
      id: 'time',
      header: t('admin.plugin-calls.time'),
      cell: ({ row }) => <span className="font-mono text-xs whitespace-nowrap">{formatLogTime(row.original.createdAt)}</span>,
    },
    {
      id: 'who',
      header: t('admin.plugin-calls.who'),
      cell: ({ row }) => {
        const call = row.original
        if (!call.userId) return <span className="text-muted-foreground text-sm">{t('admin.usage.no-account')}</span>
        return (
          <div className="flex items-center gap-2">
            <UserAvatar id={call.userId} name={call.userName ?? undefined} email={call.userEmail ?? undefined} className="size-6 text-xs" />
            <LongText className="max-w-36">{call.userName || call.userEmail}</LongText>
          </div>
        )
      },
    },
    {
      id: 'tool',
      header: t('admin.plugin-calls.tool'),
      cell: ({ row }) => (
        <div className="flex flex-col">
          <span className="text-sm">{row.original.pluginName ?? row.original.plugin}</span>
          <span className="text-muted-foreground font-mono text-xs">{row.original.tool}</span>
        </div>
      ),
    },
    {
      id: 'result',
      header: t('admin.plugin-calls.result'),
      cell: ({ row }) => {
        const call = row.original
        return (
          <div className="flex flex-wrap items-center gap-1.5">
            {call.ok ? (
              <Badge variant="secondary">{t('admin.plugin-calls.ok')}</Badge>
            ) : (
              <Badge variant="destructive">{t(`admin.plugin-calls.outcome.${call.outcome}`, { defaultValue: call.outcome || t('admin.plugin-calls.failed') })}</Badge>
            )}
            {call.confirmed && <Badge variant="outline">{t('admin.plugin-calls.confirmed')}</Badge>}
          </div>
        )
      },
    },
    {
      id: 'duration',
      header: t('admin.plugin-calls.duration'),
      cell: ({ row }) => (
        <span className="font-mono text-xs">{row.original.durationMs > 0 ? `${(row.original.durationMs / 1000).toFixed(1)}s` : DASH}</span>
      ),
    },
  ]
}

export default PluginCalls
