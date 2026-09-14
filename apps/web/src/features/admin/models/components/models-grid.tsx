import { Link } from '@tanstack/react-router'
import { Boxes, ChevronLeft, ChevronRight, PackageOpen, Plug, type LucideIcon } from 'lucide-react'
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
   * Whether a channel has been configured at all. `undefined` while the channel
   * list is still being read.
   */
  hasChannels: boolean | undefined
  /**
   * Whether the configured channels serve anything — the answer to the
   * unfiltered channel-scope probe, not to the query behind this grid.
   * `undefined` while that question is still open.
   */
  hasModels: boolean | undefined
  /** True while either question above is still in flight. */
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
  hasModels,
  isReachPending,
  onBrowseAll,
}: ModelsGridProps) {
  const { t } = useTranslation()
  const pageCount = Math.max(1, Math.ceil(total / limit))

  if (isError) return <Placeholder message={t('state.error')} />

  if (isPending) return <LoadingGrid label={t('state.loading')} />

  if (models.length === 0) {
    if (scope === 'channels') {
      // An empty grid means three very different things here, and none of them
      // is visible in this query: "nothing matched your filters", "no channel
      // has been added", and "a channel is there but nobody has fetched what it
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

      // The channel exists and lists nothing, which is where every new channel
      // starts: what it serves is its own statement, and nobody has asked it
      // for one yet. Saying "no matching models" here would blame the filters
      // for an empty list that no filter could have changed.
      if (hasModels === false) {
        return (
          <Placeholder
            icon={PackageOpen}
            message={t('admin.channels.models-empty-hint')}
            action={
              <Button asChild size="sm">
                <Link to="/admin/channels">{t('admin.nav.channels')}</Link>
              </Button>
            }
          />
        )
      }
    } else if (isCatalogueEmpty) {
      // Before the first sync there is nothing to browse and nothing to clear —
      // the only useful thing on the page is the refresh, so it goes here too.
      //
      // Only on this tab. The channel tab's list comes from the channels, not
      // from models.dev, so an unsynced catalogue is not the reason it is empty
      // there — sending that operator to the refresh button would be sending
      // them to the wrong page.
      return (
        <Placeholder message={t('admin.models.never-synced')} action={<CatalogRefreshButton />} />
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
