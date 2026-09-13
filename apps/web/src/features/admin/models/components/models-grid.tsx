import { Link } from '@tanstack/react-router'
import { Boxes, ChevronLeft, ChevronRight, Plug, type LucideIcon } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/shadcn/ui/button'
import { Card } from '@/components/shadcn/ui/card'
import { Skeleton } from '@/components/shadcn/ui/skeleton'
import { CatalogRefreshButton } from './catalog-refresh-button'
import { ModelCard } from './model-card'
import type { CatalogModel, CatalogScope } from '../use-catalog'

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
  /** True when the catalogue itself is empty, not merely the current filter. */
  isCatalogueEmpty: boolean
  /** Which catalogue is being listed; only `channels` can run out of channels. */
  scope: CatalogScope
  /**
   * Whether any configured channel reaches anything — the answer to the
   * unfiltered channel-scope probe, not to the query behind this grid.
   * `undefined` while that question is still open.
   */
  hasChannels: boolean | undefined
  /** True while the probe above is in flight. */
  isReachPending: boolean
  /** Switches the page to the full catalogue, for the no-channels state. */
  onBrowseAll: () => void
}

export function ModelsGrid({
  models,
  total,
  page,
  limit,
  onPageChange,
  isPending,
  isError,
  isCatalogueEmpty,
  scope,
  hasChannels,
  isReachPending,
  onBrowseAll,
}: ModelsGridProps) {
  const { t } = useTranslation()
  const pageCount = Math.max(1, Math.ceil(total / limit))

  if (isError) return <Placeholder message={t('state.error')} />

  if (isPending) return <LoadingGrid label={t('state.loading')} />

  if (models.length === 0) {
    // Before the first sync there is nothing to browse and nothing to clear —
    // the only useful thing on the page is the refresh, so it goes here too.
    // This outranks the channel question: with an empty catalogue every scope is
    // empty, and blaming the channels would send the operator to the wrong page.
    if (isCatalogueEmpty) {
      return (
        <Placeholder message={t('admin.models.never-synced')} action={<CatalogRefreshButton />} />
      )
    }

    if (scope === 'channels') {
      // An empty grid means two very different things here, and the difference
      // is not visible in this query: "nothing matched your filters" and "there
      // is nothing to match, because no channel has been added". Only the
      // unfiltered probe separates them, so wait for it rather than guess — a
      // wrong message flashed for a frame is worse than a frame of skeleton.
      if (isReachPending) return <LoadingGrid label={t('state.loading')} />

      if (hasChannels === false) {
        return (
          <Placeholder
            icon={Plug}
            message={t('admin.models.no-channels')}
            action={
              <div className="flex flex-wrap items-center justify-center gap-2">
                <Button asChild size="sm">
                  <Link to="/admin/channels">{t('admin.models.go-to-channels')}</Link>
                </Button>
                {/* Adding a channel is the fix; browsing the catalogue is how
                    you decide which one to add. Secondary, but on the same
                    screen, because the two questions arrive together. */}
                <Button variant="ghost" size="sm" onClick={onBrowseAll}>
                  {t('admin.models.browse-all')}
                </Button>
              </div>
            }
          />
        )
      }
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
