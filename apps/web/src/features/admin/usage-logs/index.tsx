import { useTranslation } from 'react-i18next'

import { ConfigDrawer } from '@/components/shadcn/config-drawer'
import { Header } from '@/components/shadcn/layout/header'
import { Main } from '@/components/shadcn/layout/main'
import { ProfileDropdown } from '@/components/shadcn/profile-dropdown'
import { Search } from '@/components/shadcn/search'
import { UsageLogsTable } from './components/usage-logs-table'
import type { UsageFilters } from './use-usage-logs'

export function UsageLogs({ initialFilters }: { initialFilters?: UsageFilters }) {
  const { t } = useTranslation()

  return (
    <>
      <Header fixed>
        <Search className="me-auto" />
        <ConfigDrawer />
        <ProfileDropdown />
      </Header>

      <Main className="flex flex-1 flex-col gap-4 sm:gap-6">
        <div>
          <h2 className="text-2xl font-bold tracking-tight">{t('admin.nav.usage-logs')}</h2>
          <p className="text-muted-foreground">{t('admin.usage.subtitle')}</p>
        </div>
        <UsageLogsTable initialFilters={initialFilters} />
      </Main>
    </>
  )
}

export default UsageLogs
