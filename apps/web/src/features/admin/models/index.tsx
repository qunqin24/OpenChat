import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { ConfigDrawer } from '@/components/shadcn/config-drawer'
import { Header } from '@/components/shadcn/layout/header'
import { Main } from '@/components/shadcn/layout/main'
import { Search } from '@/components/shadcn/search'
import { Tabs, TabsList, TabsTrigger } from '@/components/shadcn/ui/tabs'
import { CatalogStatusLine } from './components/catalog-status-line'
import { ModelHealthGrid } from './components/model-health'
import { ModelsGrid } from './components/models-grid'
import { ModelsToolbar } from './components/models-toolbar'
import { useCatalogModels, useChannelReach, useHasChannels } from './use-catalog'

/** Divides evenly into two, three and four columns, so no row ends ragged. */
const PAGE_SIZE = 24

/**
 * The model marketplace: which models this installation can actually reach, and
 * which of them it offers. A grid rather than a table because the decision is a
 * comparison between brands — the mark, the blurb and what the model can do are
 * the columns that matter, and none of them fit a cell.
 *
 * It lists one thing: the models a configured channel serves. Browsing the
 * whole models.dev catalogue used to sit beside it on a second tab, and it is
 * deliberately gone — the catalogue runs to thousands of models this
 * installation cannot answer with, so it is a reference work rather than an
 * inventory, and the question it answered (which channel to add next) is one
 * the channel form answers where the decision is actually made. It survives as
 * optional metadata on these cards; it supplies neither their membership nor
 * any marketplace controls.
 *
 * Searching, filtering and paging are all the server's, exactly as the user
 * table does it.
 *
 * A second tab shows how each model has done in the last six hours - speed,
 * time to the first token, success - from the usage ledger (model-health.tsx).
 */
export function Models() {
  const { t } = useTranslation()

  const [tab, setTab] = useState<'models' | 'recent'>('models')
  const [search, setSearch] = useState('')
  const [enabledOnly, setEnabledOnly] = useState(false)
  const [page, setPage] = useState(1)

  const filters = { page, limit: PAGE_SIZE, search, enabledOnly }
  const { data, isPending, isError } = useCatalogModels(filters)
  // Asked once, unfiltered, so an empty grid can be attributed: no channels at
  // all, a channel whose model list nobody has fetched, or no model matching
  // what was typed. The query above cannot tell the three apart — all of them
  // come back as zero items.
  const { hasChannels, isPending: isChannelsPending } = useHasChannels()
  const { data: hasModels, isPending: isReachPending } = useChannelReach()

  // A narrowed filter can leave the cursor past the last page, so every filter
  // change rewinds it. Done in the handlers rather than an effect: the reset is
  // part of the same user action, not a reaction to one.
  const changeSearch = (value: string) => {
    setSearch(value)
    setPage(1)
  }
  const changeEnabledOnly = (value: boolean) => {
    setEnabledOnly(value)
    setPage(1)
  }

  return (
    <>
      <Header fixed>
        <Search className="me-auto" />
        <ConfigDrawer />
      </Header>

      <Main className="flex flex-1 flex-col gap-4 sm:gap-6">
        <div className="flex flex-wrap items-end justify-between gap-2">
          <div className="flex flex-wrap items-center gap-4">
            <h2 className="text-2xl font-bold tracking-tight">{t('admin.models.title')}</h2>
            <Tabs value={tab} onValueChange={(value) => setTab(value as 'models' | 'recent')}>
              <TabsList>
                <TabsTrigger value="models">{t('admin.models.tab-models')}</TabsTrigger>
                <TabsTrigger value="recent">{t('admin.models.tab-recent')}</TabsTrigger>
              </TabsList>
            </Tabs>
          </div>
          {tab === 'models' && <CatalogStatusLine />}
        </div>

        {tab === 'recent' ? (
          <>
            {/* The same words as the models tab: switching keeps the search. */}
            <ModelsToolbar search={search} onSearchChange={changeSearch} />
            <ModelHealthGrid search={search} />
          </>
        ) : (
          <>
            <ModelsToolbar
              search={search}
              onSearchChange={changeSearch}
              enabledOnly={enabledOnly}
              onEnabledOnlyChange={changeEnabledOnly}
            />

            <ModelsGrid
              models={data?.items ?? []}
              total={data?.total ?? 0}
              page={page}
              limit={PAGE_SIZE}
              onPageChange={setPage}
              isPending={isPending}
              isError={isError}
              hasChannels={hasChannels}
              hasModels={hasModels}
              isReachPending={isChannelsPending || isReachPending}
            />
          </>
        )}
      </Main>
    </>
  )
}

export default Models
