import { ArrowLeft, LayoutDashboard, Settings, Users } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { IconAperture } from '@/assets/custom/icon-aperture'

import { type SidebarData } from '../types'

/**
 * Navigation for the admin panel.
 *
 * A hook rather than the constant upstream exports, because every label here
 * is translated and `t` is only available inside a component. The shape is
 * unchanged, so NavGroup, TeamSwitcher and CommandMenu consume it as before.
 * The `user` block is a fallback — AppSidebar overrides it with the session.
 */
export function useSidebarData(): SidebarData {
  const { t } = useTranslation()

  return {
    user: { name: '', email: '', avatar: '' },
    teams: [
      {
        name: 'OpenChat',
        logo: IconAperture,
        plan: t('admin.title'),
      },
    ],
    navGroups: [
      {
        title: t('admin.ui.nav-group.general'),
        items: [
          { title: t('admin.nav.dashboard'), url: '/admin', icon: LayoutDashboard },
          { title: t('admin.nav.users'), url: '/admin/users', icon: Users },
        ],
      },
      {
        title: t('admin.ui.nav-group.other'),
        items: [
          { title: t('admin.ui.menu.settings'), url: '/admin/settings', icon: Settings },
          { title: t('admin.back-to-chat'), url: '/', icon: ArrowLeft },
        ],
      },
    ],
  }
}
