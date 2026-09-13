import { useTranslation } from 'react-i18next'

import { ConfigDrawer } from '@/components/shadcn/config-drawer'
import { Header } from '@/components/shadcn/layout/header'
import { Main } from '@/components/shadcn/layout/main'
import { ProfileDropdown } from '@/components/shadcn/profile-dropdown'
import { Search } from '@/components/shadcn/search'
import { ChannelsPrimaryButtons } from './components/channels-primary-buttons'
import { ChannelsTable } from './components/channels-table'

export function Channels() {
  const { t } = useTranslation()

  return (
    <>
      <Header fixed>
        <Search className="me-auto" />
        <ConfigDrawer />
        <ProfileDropdown />
      </Header>

      <Main className="flex flex-1 flex-col gap-4 sm:gap-6">
        <div className="flex flex-wrap items-end justify-between gap-2">
          <div>
            <h2 className="text-2xl font-bold tracking-tight">{t('admin.channels.title')}</h2>
            <p className="text-muted-foreground">{t('admin.channels.subtitle')}</p>
          </div>
          <ChannelsPrimaryButtons />
        </div>
        <ChannelsTable />
      </Main>
    </>
  )
}

export default Channels
