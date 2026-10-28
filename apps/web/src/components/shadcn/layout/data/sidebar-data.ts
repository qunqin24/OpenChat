import { ArrowLeft, Boxes, Globe, HardDrive, ImageIcon, KeyRound, LayoutDashboard, Plug, ScrollText, Settings, ShieldAlert, Users } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { type SidebarData } from '../types'

/**
 * Navigation for the admin panel.
 *
 * A hook rather than the constant upstream exports, because every label here
 * is translated and `t` is only available inside a component. The shape is
 * unchanged otherwise, so NavGroup and CommandMenu consume it as before; the
 * upstream `teams` list is gone with the team switcher (see AppBrand).
 * The `user` block is a fallback — AppSidebar overrides it with the session.
 */
export function useSidebarData(): SidebarData {
  const { t } = useTranslation()

  return {
    user: { id: '', name: '', email: '' },
    navGroups: [
      {
        title: t('admin.ui.nav-group.general'),
        items: [
          { title: t('admin.nav.dashboard'), url: '/admin', icon: LayoutDashboard },
          { title: t('admin.nav.users'), url: '/admin/users', icon: Users },
          { title: t('admin.nav.authentication'), url: '/admin/authentication', icon: KeyRound },
          { title: t('admin.nav.channels'), url: '/admin/channels', icon: Plug },
          { title: t('admin.nav.models'), url: '/admin/models', icon: Boxes },
          { title: t('admin.nav.usage-logs'), url: '/admin/usage-logs', icon: ScrollText },
          { title: t('admin.nav.moderation-logs'), url: '/admin/moderation-logs', icon: ShieldAlert },
          { title: t('admin.nav.web-search'), url: '/admin/web-search', icon: Globe },
          { title: t('admin.nav.image-search'), url: '/admin/image-search', icon: ImageIcon },
          { title: t('admin.nav.storage'), url: '/admin/storage', icon: HardDrive },
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
