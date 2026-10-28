import { type Column, type ColumnDef } from '@tanstack/react-table'
import type { TFunction } from 'i18next'

import { ModelIcon } from '@/components/model-icon'
import { DataTableColumnHeader } from '@/components/shadcn/data-table'
import { LongText } from '@/components/shadcn/long-text'
import { Badge } from '@/components/shadcn/ui/badge'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/shadcn/ui/tooltip'
import { UserAvatar } from '@/components/user-avatar'
import { formatCost } from '@/features/admin/models/format'
import { cn } from '@/lib/utils'
import {
  durationTone,
  firstTokenTone,
  cacheHitRate,
  formatCount,
  formatDuration,
  formatPercent,
  formatLogTime,
  formatUsd,
  knownErrorCode,
  outputSpeed,
  rejectedAtOnce,
  toneClass,
} from '../format'
import type { UsageLog } from '../use-usage-logs'

const DASH = '—'

/**
 * Built from `t` rather than calling useTranslation in each cell, so the column
 * set stays a plain value the table can memoise.
 *
 * There is no token column: this app has no API tokens, only signed-in people.
 */
export function usageLogsColumns(t: TFunction<'common'>): ColumnDef<UsageLog>[] {
  const header =
    (title: string) =>
    ({ column }: { column: Column<UsageLog, unknown> }) => (
      <DataTableColumnHeader column={column} title={title} />
    )

  return [
    {
      id: 'time',
      meta: { title: t('admin.usage.time') },
      header: header(t('admin.usage.time')),
      cell: ({ row }) => {
        const log = row.original
        return (
          <div className="flex flex-col items-start gap-1">
            <span className="font-mono text-xs whitespace-nowrap">{formatLogTime(log.createdAt)}</span>
            <span className="flex items-center gap-1">
              {log.status === 'error' ? (
                <Badge variant="destructive" className="px-1.5 py-0 text-[11px]">
                  {t('admin.usage.status-error')}
                </Badge>
              ) : (
                <Badge variant="outline" className="px-1.5 py-0 text-[11px]">
                  {t(`admin.usage.kind-${log.kind}`)}
                </Badge>
              )}
            </span>
          </div>
        )
      },
      enableSorting: false,
      enableHiding: false,
    },
    {
      id: 'channel',
      meta: { title: t('admin.usage.channel') },
      header: header(t('admin.usage.channel')),
      cell: ({ row }) => {
        const log = row.original
        if (!log.channelName && !log.channelId) return <span className="text-muted-foreground">{DASH}</span>
        // The id is a UUID; its first group is enough to tell channels apart
        // and to search for, and the full one is in the tooltip. A channel
        // that was deleted has no id any more, only the name the row kept.
        return (
          <div className="flex flex-col items-start gap-0.5">
            <LongText className={cn('max-w-36', !log.channelId && 'text-muted-foreground line-through')}>
              {log.channelName || DASH}
            </LongText>
            {log.channelId && (
              <Tooltip>
                <TooltipTrigger asChild>
                  <span className="text-muted-foreground font-mono text-xs">#{log.channelId.slice(0, 8)}</span>
                </TooltipTrigger>
                <TooltipContent className="font-mono">{log.channelId}</TooltipContent>
              </Tooltip>
            )}
          </div>
        )
      },
      enableSorting: false,
    },
    {
      id: 'user',
      meta: { title: t('admin.usage.user') },
      header: header(t('admin.usage.user')),
      cell: ({ row }) => {
        const log = row.original
        // No owner: an account an administrator deleted (it keeps the name it
        // had), else a row from before accounts were required.
        if (!log.userId && log.deletedUser) {
          return <span className="text-muted-foreground">{t('admin.usage.deleted-user', { name: log.deletedUser })}</span>
        }
        if (!log.userId) return <span className="text-muted-foreground">{t('admin.usage.no-account')}</span>
        return (
          <div className="flex items-center gap-2">
            <UserAvatar
              id={log.userId}
              name={log.userName ?? undefined}
              email={log.userEmail ?? undefined}
              src={log.userAvatarUrl}
              className="size-6 text-xs"
            />
            <LongText className="max-w-32">{log.userName || log.userEmail}</LongText>
          </div>
        )
      },
      enableSorting: false,
    },
    {
      id: 'model',
      meta: { title: t('admin.usage.model') },
      header: header(t('admin.usage.model')),
      cell: ({ row }) => {
        const log = row.original
        // The served model only earns a line when it differs: a gateway that
        // silently swaps the model is exactly what this column is read for.
        const served = log.servedModel && log.servedModel !== log.model ? log.servedModel : undefined
        return (
          <div className="flex items-center gap-2">
            {/* Only the id is known here, and an id carries its family:
                `claude-sonnet-4` is drawn with Claude's mark. */}
            <ModelIcon subject="model" family={log.model} name={log.model} className="size-6" />
            <div className="flex min-w-0 flex-col">
              <LongText className="max-w-44 font-medium">{log.model}</LongText>
              {served && (
                <LongText className="text-muted-foreground max-w-44 text-xs">{`→ ${served}`}</LongText>
              )}
            </div>
          </div>
        )
      },
      enableSorting: false,
    },
    {
      id: 'speed',
      meta: { title: t('admin.usage.speed-title') },
      header: header(t('admin.usage.speed-title')),
      cell: ({ row }) => {
        const speed = outputSpeed(row.original)
        return (
          <span className="text-muted-foreground text-sm whitespace-nowrap tabular-nums" title={t('admin.usage.speed-hint')}>
            {speed === undefined ? DASH : t('admin.usage.speed', { n: speed.toFixed(speed < 100 ? 1 : 0) })}
          </span>
        )
      },
      enableSorting: false,
    },
    {
      id: 'tokens',
      meta: { title: t('admin.usage.tokens') },
      header: header(t('admin.usage.tokens')),
      cell: ({ row }) => {
        const log = row.original
        if (log.status === 'error') return <ErrorCell log={log} t={t} />
        const hit = cacheHitRate(log)
        const extras = [
          log.cacheReadTokens > 0 && t('admin.usage.cache-read', { n: formatCount(log.cacheReadTokens) }),
          hit !== undefined && (log.cacheReadTokens > 0 || log.cacheWriteTokens > 0) && t('admin.usage.cache-hit', { n: formatPercent(hit) }),
          log.cacheWriteTokens > 0 && t('admin.usage.cache-write', { n: formatCount(log.cacheWriteTokens) }),
          log.reasoningTokens > 0 && t('admin.usage.reasoning', { n: formatCount(log.reasoningTokens) }),
        ].filter(Boolean)
        return (
          <div className="flex flex-col gap-0.5 tabular-nums">
            <span className="whitespace-nowrap">
              {formatCount(log.inputTokens)}
              <span className="text-muted-foreground mx-1">/</span>
              {formatCount(log.outputTokens)}
            </span>
            {extras.length > 0 && (
              <span className="text-muted-foreground text-xs whitespace-nowrap">{extras.join(' · ')}</span>
            )}
          </div>
        )
      },
      enableSorting: false,
    },
    {
      id: 'cost',
      meta: { title: t('admin.usage.cost') },
      header: header(t('admin.usage.cost')),
      cell: ({ row }) => {
        const log = row.original
        // A failed request has a cost too when it was estimated: it was taken
        // up and billed before it broke.
        if (log.cost == null) {
          return <span className="text-muted-foreground">{DASH}</span>
        }
        return (
          <span className="inline-flex items-center gap-1.5">
            <span className="font-mono text-sm tabular-nums">{formatUsd(log.cost)}</span>
            {log.estimated && (
              <Tooltip>
                <TooltipTrigger asChild>
                  <Badge variant="outline" className="text-muted-foreground px-1.5 py-0 text-[11px]" tabIndex={0}>
                    {t('admin.usage.estimated')}
                  </Badge>
                </TooltipTrigger>
                <TooltipContent className="max-w-xs">{t('admin.usage.estimated-hint')}</TooltipContent>
              </Tooltip>
            )}
          </span>
        )
      },
      enableSorting: false,
    },
    {
      id: 'timing',
      meta: { title: t('admin.usage.timing') },
      header: header(t('admin.usage.timing')),
      cell: ({ row }) => {
        const log = row.original
        return (
          <div className="flex items-center gap-1.5">
            {log.firstTokenMs != null ? (
              <Pill tone={firstTokenTone(log.firstTokenMs)} label={t('admin.usage.first-token')}>
                {formatDuration(log.firstTokenMs)}
              </Pill>
            ) : (
              <span className="text-muted-foreground text-xs">{DASH}</span>
            )}
            {rejectedAtOnce(log) ? (
              <Tooltip>
                <TooltipTrigger asChild>
                  <span className="text-muted-foreground text-xs" tabIndex={0}>
                    {DASH}
                  </span>
                </TooltipTrigger>
                <TooltipContent>{t('admin.usage.no-duration')}</TooltipContent>
              </Tooltip>
            ) : (
              <Pill tone={durationTone(log.durationMs)} label={t('admin.usage.duration')}>
                {formatDuration(log.durationMs)}
              </Pill>
            )}
          </div>
        )
      },
      enableSorting: false,
    },
    {
      id: 'details',
      meta: { title: t('admin.usage.details') },
      header: header(t('admin.usage.details')),
      cell: ({ row }) => {
        const log = row.original
        if (log.inputPrice == null || log.outputPrice == null) {
          return <span className="text-muted-foreground">{DASH}</span>
        }
        const cache = [
          log.cacheReadPrice != null && `${t('admin.usage.cache-read-price')} $${formatCost(log.cacheReadPrice)}`,
          log.cacheWritePrice != null && `${t('admin.usage.cache-write-price')} $${formatCost(log.cacheWritePrice)}`,
        ].filter(Boolean)
        return (
          <Tooltip>
            <TooltipTrigger asChild>
              <div className="flex flex-col gap-0.5 text-xs whitespace-nowrap">
                <span>
                  {t('admin.usage.tier-standard')}
                  <span className="text-muted-foreground mx-1">·</span>
                  <span className="tabular-nums">
                    ${formatCost(log.inputPrice)} / ${formatCost(log.outputPrice)}
                  </span>
                </span>
                {cache.length > 0 && <span className="text-muted-foreground">{cache.join(' · ')}</span>}
              </div>
            </TooltipTrigger>
            <TooltipContent>{t('admin.usage.price-hint')}</TooltipContent>
          </Tooltip>
        )
      },
      enableSorting: false,
    },
  ]
}

function Pill({
  tone,
  label,
  children,
}: {
  tone: 'fast' | 'medium' | 'slow'
  label: string
  children: React.ReactNode
}) {
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <span
          className={cn(
            'rounded-md px-1.5 py-0.5 font-mono text-xs whitespace-nowrap tabular-nums',
            toneClass[tone],
          )}
        >
          {children}
        </span>
      </TooltipTrigger>
      <TooltipContent>{label}</TooltipContent>
    </Tooltip>
  )
}

/**
 * A failed request has no tokens or cost to show. In their place: what went
 * wrong, in a word, with the chat's own sentence for the code and the
 * provider's (scrubbed) message in the tooltip.
 */
function ErrorCell({ log, t }: { log: UsageLog; t: TFunction<'common'> }) {
  const known = knownErrorCode(log.errorCode)
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <span className="text-destructive block max-w-56 truncate text-sm font-medium" tabIndex={0}>
          {known ? t(`admin.usage.error-code.${known}`) : (log.errorCode ?? t('admin.usage.status-error'))}
        </span>
      </TooltipTrigger>
      <TooltipContent className="max-w-sm">
        <div className="flex flex-col gap-1">
          {known && <span>{t(`chat.error.${known}`)}</span>}
          {log.errorMessage && <span className="font-mono opacity-80">{log.errorMessage}</span>}
          {log.errorCode && <span className="font-mono opacity-60">{log.errorCode}</span>}
        </div>
      </TooltipContent>
    </Tooltip>
  )
}
