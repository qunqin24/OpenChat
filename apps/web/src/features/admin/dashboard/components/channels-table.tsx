import type { AdminDashboard } from '@openchat/api-client'
import { useTranslation } from 'react-i18next'

import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/shadcn/ui/table'
import {
  durationTone,
  firstTokenTone,
  formatCount,
  formatDuration,
  formatLogTime,
  toneClass,
  type Tone,
} from '@/features/admin/usage-logs/format'
import { formatRelative } from '@openchat/chat-core/grouping'
import { cn } from '@/lib/utils'
import { errorRate, errorRateClass, formatRate } from '../format'
import { Empty } from './empty'

/** `P50 / P95`, the first toned by how quick it is, the tail in grey. */
function Latency({ p50, p95, tone }: { p50?: number | null; p95?: number | null; tone: (ms: number) => Tone }) {
  if (p50 == null) return <span className="text-muted-foreground">—</span>
  return (
    <span className="inline-flex items-center justify-end gap-1.5 whitespace-nowrap tabular-nums">
      <span className={cn('rounded-md px-1.5 py-0.5 text-xs', toneClass[tone(p50)])}>{formatDuration(p50)}</span>
      <span className="text-muted-foreground text-xs">/ {p95 == null ? '—' : formatDuration(p95)}</span>
    </span>
  )
}

/**
 * One row per upstream channel, the failing one first to the eye: its error
 * rate in colour and when it last failed. Latencies are of the successful
 * requests.
 */
export function ChannelsTable({ channels }: { channels: AdminDashboard['channels'] }) {
  const { t } = useTranslation()
  if (channels.length === 0) return <Empty />

  return (
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead>{t('admin.dashboard.channels.channel')}</TableHead>
          <TableHead className="text-right">{t('admin.dashboard.channels.requests')}</TableHead>
          <TableHead className="text-right">{t('admin.dashboard.channels.errors')}</TableHead>
          <TableHead className="text-right">{t('admin.dashboard.channels.first-token')}</TableHead>
          <TableHead className="text-right">{t('admin.dashboard.channels.duration')}</TableHead>
          <TableHead className="text-right">{t('admin.dashboard.channels.last-error')}</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {channels.map((c) => {
          const rate = errorRate(c.errors, c.requests)
          return (
            <TableRow key={c.channelId ?? c.channelName}>
              <TableCell className="max-w-48 truncate font-medium" title={c.channelName}>
                {c.channelName}
              </TableCell>
              <TableCell className="text-right tabular-nums">{formatCount(c.requests)}</TableCell>
              <TableCell className={cn('text-right tabular-nums', errorRateClass(rate))}>
                {formatRate(rate)}
                {c.errors > 0 && <span className="text-muted-foreground ms-1 text-xs font-normal">({formatCount(c.errors)})</span>}
              </TableCell>
              <TableCell className="text-right">
                <Latency p50={c.firstTokenP50Ms} p95={c.firstTokenP95Ms} tone={firstTokenTone} />
              </TableCell>
              <TableCell className="text-right">
                <Latency p50={c.durationP50Ms} p95={c.durationP95Ms} tone={durationTone} />
              </TableCell>
              <TableCell className="text-muted-foreground text-right text-xs whitespace-nowrap">
                {c.lastErrorAt ? <span title={formatLogTime(c.lastErrorAt)}>{formatRelative(c.lastErrorAt, t)}</span> : '—'}
              </TableCell>
            </TableRow>
          )
        })}
      </TableBody>
    </Table>
  )
}
