import { Link, Outlet, useLocation, useNavigate } from '@tanstack/react-router'
import { ArrowLeft, LayoutDashboard, LogOut, Users } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import {
  Sidebar,
  SidebarContent,
  SidebarFooter,
  SidebarGroup,
  SidebarGroupContent,
  SidebarGroupLabel,
  SidebarHeader,
  SidebarInset,
  SidebarMenu,
  SidebarMenuButton,
  SidebarMenuItem,
  SidebarProvider,
  SidebarRail,
  SidebarTrigger,
} from '@/components/shadcn/ui/sidebar'
import { Separator } from '@/components/shadcn/ui/separator'
import { Toaster } from '@/components/shadcn/ui/sonner'
import { useLogout, useSession } from '@/features/auth/use-auth'

/**
 * The admin panel keeps shadcn's own look rather than the prism tokens the chat
 * UI uses, so everything under here is wrapped in `.shadcn-scope` — the hook
 * that confines shadcn's base layer (see styles/shadcn-base.css).
 */
export function AdminLayout() {
  const { t } = useTranslation()
  const location = useLocation()
  const navigate = useNavigate()
  const { data: session } = useSession()
  const logout = useLogout()

  const nav = [
    { to: '/admin', label: t('admin.nav.dashboard'), icon: LayoutDashboard, exact: true },
    { to: '/admin/users', label: t('admin.nav.users'), icon: Users, exact: false },
  ] as const

  return (
    <div className="shadcn-scope min-h-svh">
      <SidebarProvider>
        <Sidebar collapsible="icon">
          <SidebarHeader>
            <div className="flex items-center gap-2 px-2 py-1.5">
              <div className="bg-primary text-primary-foreground flex size-7 shrink-0 items-center justify-center rounded-md text-xs font-semibold">
                OC
              </div>
              <div className="grid flex-1 text-left leading-tight group-data-[collapsible=icon]:hidden">
                <span className="truncate text-sm font-semibold">{t('app.name')}</span>
                <span className="text-muted-foreground truncate text-xs">{t('admin.title')}</span>
              </div>
            </div>
          </SidebarHeader>

          <SidebarContent>
            <SidebarGroup>
              <SidebarGroupLabel>{t('admin.nav.manage')}</SidebarGroupLabel>
              <SidebarGroupContent>
                <SidebarMenu>
                  {nav.map((item) => (
                    <SidebarMenuItem key={item.to}>
                      <SidebarMenuButton
                        asChild
                        tooltip={item.label}
                        isActive={
                          item.exact
                            ? location.pathname === item.to
                            : location.pathname.startsWith(item.to)
                        }
                      >
                        <Link to={item.to}>
                          <item.icon />
                          <span>{item.label}</span>
                        </Link>
                      </SidebarMenuButton>
                    </SidebarMenuItem>
                  ))}
                </SidebarMenu>
              </SidebarGroupContent>
            </SidebarGroup>
          </SidebarContent>

          <SidebarFooter>
            <SidebarMenu>
              <SidebarMenuItem>
                <SidebarMenuButton asChild tooltip={t('admin.back-to-chat')}>
                  <Link to="/">
                    <ArrowLeft />
                    <span>{t('admin.back-to-chat')}</span>
                  </Link>
                </SidebarMenuButton>
              </SidebarMenuItem>
              <SidebarMenuItem>
                <SidebarMenuButton
                  tooltip={t('auth.logout')}
                  onClick={async () => {
                    await logout.mutateAsync()
                    await navigate({ to: '/login' })
                  }}
                >
                  <LogOut />
                  <span>{t('auth.logout')}</span>
                </SidebarMenuButton>
              </SidebarMenuItem>
            </SidebarMenu>
          </SidebarFooter>

          <SidebarRail />
        </Sidebar>

        <SidebarInset className="@container/content">
          <header className="flex h-14 shrink-0 items-center gap-2 border-b px-4">
            <SidebarTrigger className="-ms-1" />
            <Separator orientation="vertical" className="me-2 h-4" />
            <span className="text-sm font-medium">{t('admin.title')}</span>
            <span className="text-muted-foreground ms-auto text-sm">{session?.name}</span>
          </header>

          <Outlet />
        </SidebarInset>
      </SidebarProvider>

      <Toaster position="bottom-right" />
    </div>
  )
}

export default AdminLayout
