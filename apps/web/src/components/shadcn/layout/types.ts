import { type LinkProps } from '@tanstack/react-router'

// Upstream (satnaing/shadcn-admin) also has collapsible items with children and
// a badge per item. This panel's navigation is one flat list, so only the link
// is kept; NavGroup and CommandMenu draw nothing else.
type NavItem = {
  title: string
  url: LinkProps['to'] | (string & {})
  icon?: React.ElementType
}

type NavGroup = {
  title: string
  items: NavItem[]
}

type SidebarData = {
  navGroups: NavGroup[]
}

export type { SidebarData, NavGroup, NavItem }
