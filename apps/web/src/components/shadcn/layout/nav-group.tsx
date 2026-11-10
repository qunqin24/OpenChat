import { Link, useLocation } from '@tanstack/react-router'
import {
  SidebarGroup,
  SidebarGroupLabel,
  SidebarMenu,
  SidebarMenuButton,
  SidebarMenuItem,
  useSidebar,
} from '@/components/shadcn/ui/sidebar'
import { type NavItem, type NavGroup as NavGroupProps } from './types'

export function NavGroup({ title, items }: NavGroupProps) {
  const href = useLocation({ select: (location) => location.href })
  return (
    <SidebarGroup>
      <SidebarGroupLabel>{title}</SidebarGroupLabel>
      <SidebarMenu>
        {items.map((item) => (
          <SidebarMenuLink key={`${item.title}-${item.url}`} item={item} href={href} />
        ))}
      </SidebarMenu>
    </SidebarGroup>
  )
}

function SidebarMenuLink({ item, href }: { item: NavItem; href: string }) {
  const { setOpenMobile } = useSidebar()
  return (
    <SidebarMenuItem>
      <SidebarMenuButton
        asChild
        isActive={checkIsActive(href, item)}
        tooltip={item.title}
      >
        <Link to={item.url} onClick={() => setOpenMobile(false)}>
          {item.icon && <item.icon />}
          <span>{item.title}</span>
        </Link>
      </SidebarMenuButton>
    </SidebarMenuItem>
  )
}

function checkIsActive(href: string, item: NavItem) {
  return (
    href === item.url || // /endpint?search=param
    href.split('?')[0] === item.url // endpoint
  )
}
