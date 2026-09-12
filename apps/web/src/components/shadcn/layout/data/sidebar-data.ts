import {
  ArrowLeft,
  LayoutDashboard,
  MessagesSquare,
  Settings,
  ShieldCheck,
  Users,
} from 'lucide-react'

import { type SidebarData } from '../types'

/**
 * Navigation for the admin panel.
 *
 * Same shape as upstream's so the vendored NavGroup, TeamSwitcher and
 * CommandMenu work unchanged; the entries are ours. The `user` block here is a
 * fallback — AppSidebar overrides it with the real session.
 */
export const sidebarData: SidebarData = {
  user: {
    name: '',
    email: '',
    avatar: '',
  },
  teams: [
    {
      name: 'OpenChat',
      logo: MessagesSquare,
      plan: 'Admin',
    },
  ],
  navGroups: [
    {
      title: 'General',
      items: [
        {
          title: 'Dashboard',
          url: '/admin',
          icon: LayoutDashboard,
        },
        {
          title: 'Users',
          url: '/admin/users',
          icon: Users,
        },
      ],
    },
    {
      title: 'Other',
      items: [
        {
          title: 'Settings',
          url: '/admin/settings',
          icon: Settings,
        },
        {
          title: 'Back to chat',
          url: '/',
          icon: ArrowLeft,
        },
      ],
    },
  ],
}

/** Re-exported for the command menu, which groups by the same data. */
export const adminIcon = ShieldCheck
