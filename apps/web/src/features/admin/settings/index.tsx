import { Outlet } from '@tanstack/react-router'
import { Bug, Palette, UserCog, Wrench } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { ConfigDrawer } from '@/components/shadcn/config-drawer'
import { Header } from '@/components/shadcn/layout/header'
import { Main } from '@/components/shadcn/layout/main'
import { ProfileDropdown } from '@/components/shadcn/profile-dropdown'
import { Search } from '@/components/shadcn/search'
import { Separator } from '@/components/shadcn/ui/separator'
import { SidebarNav } from './components/sidebar-nav'

/**
 * Upstream's settings shell, with two of its five sections left out rather
 * than stubbed: Notifications has nothing to notify about, and Display toggles
 * which sidebar items appear — which the config drawer already does, from
 * every page.
 */
export function Settings() {
  const { t } = useTranslation()

  const items = [
    { title: t('admin.settings.profile.title'), href: '/admin/settings', icon: <UserCog size={18} /> },
    {
      title: t('admin.settings.account.title'),
      href: '/admin/settings/account',
      icon: <Wrench size={18} />,
    },
    {
      title: t('admin.settings.appearance.title'),
      href: '/admin/settings/appearance',
      icon: <Palette size={18} />,
    },
    {
      title: t('admin.settings.system.title'),
      href: '/admin/settings/system',
      icon: <Bug size={18} />,
    },
  ]

  return (
    <>
      <Header fixed>
        <Search className="me-auto" />
        <ConfigDrawer />
        <ProfileDropdown />
      </Header>

      <Main fixed>
        <div className="space-y-0.5">
          <h1 className="text-2xl font-bold tracking-tight md:text-3xl">
            {t('admin.ui.menu.settings')}
          </h1>
          <p className="text-muted-foreground">{t('admin.settings.subtitle')}</p>
        </div>
        <Separator className="my-4 lg:my-6" />
        <div className="flex flex-1 flex-col space-y-2 overflow-hidden md:space-y-2 lg:flex-row lg:space-y-0 lg:space-x-12">
          <aside className="top-0 lg:sticky lg:w-1/5">
            <SidebarNav items={items} />
          </aside>
          <div className="flex w-full overflow-y-hidden p-1">
            <Outlet />
          </div>
        </div>
      </Main>
    </>
  )
}

export default Settings
