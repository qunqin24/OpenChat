import { Link } from '@tanstack/react-router'
import { Boxes, ChevronLeft, ChevronRight, PackageOpen, Plug, type LucideIcon } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/shadcn/ui/button'
import { Card } from '@/components/shadcn/ui/card'
import { Skeleton } from '@/components/shadcn/ui/skeleton'
import { ModelCard } from './model-card'
import type { CatalogModel } from '../use-catalog'

const GRID =
  'grid grid-cols-1 gap-4 @xl/content:grid-cols-2 @4xl/content:grid-cols-3 @6xl/content:grid-cols-4'

function CardSkeleton() {
  return (
    <Card className="h-56 gap-3 p-4">
      <div className="flex items-center gap-3">
        <Skeleton className="size-8 rounded-md" />
        <div className="flex-1 space-y-2">
          <Skeleton className="h-4 w-2/3" />
          <Skeleton className="h-3 w-1/3" />
        </div>
      </div>
      <Skeleton className="h-3 w-full" />
      <Skeleton className="h-3 w-4/5" />
      <Skeleton className="mt-auto h-8 w-full" />
    </Card>
  )
}

function LoadingGrid({ label }: { label: string }) {
  return (
    <div className={GRID} aria-busy="true" aria-label={label}>
      {Array.from({ length: 8 }).map((_, index) => (
        <CardSkeleton key={index} />
      ))}
    </div>
  )
}

function Placeholder({
  message,
  icon: Icon = Boxes,
  action,
}: {
  message: string
  icon?: LucideIcon
  action?: React.ReactNode
}) {
  return (
    <div className="flex flex-col items-center justify-center gap-3 rounded-md border border-dashed px-6 py-16 text-center">
      <Icon className="text-muted-foreground size-8" aria-hidden="true" />
      <p className="text-muted-foreground max-w-sm text-sm text-balance">{message}</p>
      {action}
    </div>
  )
}

type ModelsGridProps = {
  models: CatalogModel[]
  total: number
  page: number
  limit: number
  onPageChange: (page: number) => void
  isPending: boolean
  isError: boolean
  /**
   * Whether a channel has been configured at all. `undefined` while the channel
   * list is still being read.
   */
  hasChannels: boolean | undefined
  /**
   * Whether the configured channels serve anything — the answer to the
   * unfiltered probe, not to the filtered query behind this grid. `undefined`
   * while that question is still open.
   */
  hasModels: boolean | undefined
  /** True while either question above is still in flight. */
  isReachPending: boolean
}

export function ModelsGrid({
  models,
  total,
  page,
  limit,
  onPageChange,
  isPending,
  isError,
  hasChannels,
  hasModels,
  isReachPending,
}: ModelsGridProps) {
  const { t } = useTranslation()
  const pageCount = Math.max(1, Math.ceil(total / limit))

  if (isError) return <Placeholder message={t('state.error')} />

  if (isPending) return <LoadingGrid label={t('state.loading')} />

  if (models.length === 0) {
    // An empty grid means three very different things here, and none of them is
    // visible in this query: "nothing matched your filters", "no channel has
    // been added", and "a channel is there but nobody has fetched what it
    // serves". Only the two unfiltered probes separate them, so wait for them
    // rather than guess — a wrong message flashed for a frame is worse than a
    // frame of skeleton.
    if (isReachPending) return <LoadingGrid label={t('state.loading')} />

    if (hasChannels === false) {
      return (
        <Placeholder
          icon={Plug}
          message={t('admin.models.no-channels')}
          action={
            <Button asChild size="sm">
              <Link to="/admin/channels">{t('admin.models.go-to-channels')}</Link>
            </Button>
          }
        />
      )
    }

    // The channel exists and lists nothing, which is where every new channel
    // starts: what it serves is its own statement, and nobody has asked it for
    // one yet. Saying "no matching models" here would blame the filters for an
    // empty list that no filter could have changed.
    if (hasModels === false) {
      return (
        <Placeholder
          icon={PackageOpen}
          message={t('admin.models.no-selected-models')}
          action={
            <Button asChild size="sm">
              <Link to="/admin/channels">{t('admin.nav.channels')}</Link>
            </Button>
          }
        />
      )
    }

    return <Placeholder message={t('admin.models.none')} />
  }

  return (
    <div className="flex flex-col gap-4">
      <div className={GRID}>
        {models.map((model) => (
          <ModelCard key={`${model.providerId}/${model.modelId}`} model={model} />
        ))}
      </div>

      <div className="flex items-center justify-between gap-2">
        <p className="text-muted-foreground text-sm tabular-nums">
          {t('admin.ui.table.page-of', { page, total: pageCount })}
        </p>
        <div className="flex items-center gap-2">
          <Button
            variant="outline"
            size="sm"
            onClick={() => onPageChange(page - 1)}
            disabled={page <= 1}
            aria-label={t('admin.ui.table.previous-page')}
          >
            <ChevronLeft aria-hidden="true" />
          </Button>
          <Button
            variant="outline"
            size="sm"
            onClick={() => onPageChange(page + 1)}
            disabled={page >= pageCount}
            aria-label={t('admin.ui.table.next-page')}
          >
            <ChevronRight aria-hidden="true" />
          </Button>
        </div>
      </div>
    </div>
  )
}
