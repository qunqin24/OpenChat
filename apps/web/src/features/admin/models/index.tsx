import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { ConfigDrawer } from '@/components/shadcn/config-drawer'
import { Header } from '@/components/shadcn/layout/header'
import { Main } from '@/components/shadcn/layout/main'
import { ProfileDropdown } from '@/components/shadcn/profile-dropdown'
import { Search } from '@/components/shadcn/search'
import { ModelsGrid } from './components/models-grid'
import { ModelsToolbar } from './components/models-toolbar'
import { useCatalogModels, useCatalogStatus } from './use-catalog'

/** Divides evenly into two, three and four columns, so no row ends ragged. */
const PAGE_SIZE = 24

/**
 * The model marketplace: what models.dev knows about, and which of them this
 * installation offers. A grid rather than a table because the decision is a
 * comparison between brands — the mark, the blurb and what the model can do are
 * the columns that matter, and none of them fit a cell.
 *
 * Searching, filtering and paging are all the server's, exactly as the user
 * table does it; the catalogue runs to thousands of rows and none of it is
 * worth shipping to the browser to slice there.
 */
export function Models() {
  const { t } = useTranslation()

  const [search, setSearch] = useState('')
  const [providerId, setProviderId] = useState('')
  const [enabledOnly, setEnabledOnly] = useState(false)
  const [page, setPage] = useState(1)

  const filters = { page, limit: PAGE_SIZE, search, providerId, enabledOnly }
  const { data, isPending, isError } = useCatalogModels(filters)
  const { data: status } = useCatalogStatus()

  // A narrowed filter can leave the cursor past the last page, so every filter
  // change rewinds it. Done in the handlers rather than an effect: the reset is
  // part of the same user action, not a reaction to one.
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

        <ModelsToolbar
          search={search}
          onSearchChange={changeSearch}
          providerId={providerId}
          onProviderChange={changeProvider}
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
          isCatalogueEmpty={status?.modelCount === 0}
        />
      </Main>
    </>
  )
}

export default Models
