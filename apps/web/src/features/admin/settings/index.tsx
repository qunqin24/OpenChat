import { ComingSoon } from '@/components/shadcn/coming-soon'
import { ConfigDrawer } from '@/components/shadcn/config-drawer'
import { Header } from '@/components/shadcn/layout/header'
import { Main } from '@/components/shadcn/layout/main'
import { ProfileDropdown } from '@/components/shadcn/profile-dropdown'
import { Search } from '@/components/shadcn/search'
import { ThemeSwitch } from '@/components/shadcn/theme-switch'

/**
 * Placeholder for everything the vendored navigation links to and we have not
 * built. Wrapped in the same Header/Main as the real pages, so the top-right
 * controls do not vanish when you land here.
 */
export function Settings() {
  return (
    <>
      <Header fixed>
        <Search className="me-auto" />
        <ThemeSwitch />
        <ConfigDrawer />
        <ProfileDropdown />
      </Header>

      <Main className="flex flex-1 flex-col">
        <ComingSoon />
      </Main>
    </>
  )
}

export default Settings
