import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { ConfigDrawer } from '@/components/shadcn/config-drawer'
import { Header } from '@/components/shadcn/layout/header'
import { Main } from '@/components/shadcn/layout/main'
import { ProfileDropdown } from '@/components/shadcn/profile-dropdown'
import { Search } from '@/components/shadcn/search'
import { Tabs, TabsContent } from '@/components/shadcn/ui/tabs'
import { ModelsGrid } from './components/models-grid'
import { ModelsToolbar } from './components/models-toolbar'
import {
  DEFAULT_SCOPE,
  SCOPES,
  useCatalogModels,
  useCatalogStatus,
  useChannelReach,
  type CatalogScope,
} from './use-catalog'

/** Divides evenly into two, three and four columns, so no row ends ragged. */
const PAGE_SIZE = 24

/**
 * The model marketplace: which models this installation can actually reach, and
 * which of them it offers. A grid rather than a table because the decision is a
 * comparison between brands — the mark, the blurb and what the model can do are
 * the columns that matter, and none of them fit a cell.
 *
 * It opens on the channel scope, not the catalogue. "Our models" means the ones
 * a configured channel can serve; the thousands of models upstream knows about
 * are a reference work, not an inventory, and they belong behind the second tab
 * — where they answer a different question, which channel to add next.
 *
 * Searching, filtering and paging are all the server's, exactly as the user
 * table does it; the catalogue runs to thousands of rows and none of it is
 * worth shipping to the browser to slice there.
 */
export function Models() {
  const { t } = useTranslation()

  const [scope, setScope] = useState<CatalogScope>(DEFAULT_SCOPE)
  const [search, setSearch] = useState('')
  const [providerId, setProviderId] = useState('')
  const [enabledOnly, setEnabledOnly] = useState(false)
  const [page, setPage] = useState(1)

  const filters = { page, limit: PAGE_SIZE, search, providerId, enabledOnly, scope }
  const { data, isPending, isError } = useCatalogModels(filters)
  const { data: status } = useCatalogStatus()
  // Asked once, unfiltered, so an empty grid can be attributed: no channels at
  // all, or no model matching what was typed. The query above cannot tell them
  // apart — both come back as zero items.
  const { data: hasChannels, isPending: isReachPending } = useChannelReach()

  // A narrowed filter can leave the cursor past the last page, so every filter
  // change rewinds it. Done in the handlers rather than an effect: the reset is
  // part of the same user action, not a reaction to one.
  const changeScope = (value: CatalogScope) => {
    setScope(value)
    setPage(1)
  }
  const changeSearch = (value: string) => {
    setSearch(value)
    setPage(1)
  }
  const changeProvider = (value: string) => {
    setProviderId(value)
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
        <ProfileDropdown />
      </Header>

      <Main className="flex flex-1 flex-col gap-4 sm:gap-6">
        <div>
          <h2 className="text-2xl font-bold tracking-tight">{t('admin.models.title')}</h2>
          <p className="text-muted-foreground">{t('admin.models.subtitle')}</p>
        </div>

        {/* The root sits here rather than in the toolbar so that each tab
            controls a panel that actually exists: the toolbar renders the list,
            and the grid below is the panel. The filters are deliberately outside
            it — they mean the same thing in both scopes and survive a switch. */}
        <Tabs
          value={scope}
          onValueChange={(value) => changeScope(value as CatalogScope)}
          className="flex flex-1 flex-col gap-4 sm:gap-6"
        >
          <ModelsToolbar
            search={search}
            onSearchChange={changeSearch}
            providerId={providerId}
            onProviderChange={changeProvider}
            enabledOnly={enabledOnly}
            onEnabledOnlyChange={changeEnabledOnly}
          />

          {SCOPES.map((value) => (
            <TabsContent key={value} value={value}>
              <ModelsGrid
                models={data?.items ?? []}
                total={data?.total ?? 0}
                page={page}
                limit={PAGE_SIZE}
                onPageChange={setPage}
                isPending={isPending}
                isError={isError}
                isCatalogueEmpty={status?.modelCount === 0}
                scope={value}
                hasChannels={hasChannels}
                isReachPending={isReachPending}
                onBrowseAll={() => changeScope('all')}
              />
            </TabsContent>
          ))}
        </Tabs>
      </Main>
    </>
  )
}

export default Models
