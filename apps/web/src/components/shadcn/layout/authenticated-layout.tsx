import { Outlet } from '@tanstack/react-router'
import { getCookie } from '@/lib/cookies'
import { cn } from '@/lib/utils'
import { LayoutProvider } from '@/features/admin/context/layout-provider'
import { ThemeProvider } from '@/features/admin/context/theme-provider'
import { DirectionProvider } from '@/features/admin/context/direction-provider'
import { SearchProvider } from '@/features/admin/context/search-provider'
import { SidebarInset, SidebarProvider } from '@/components/shadcn/ui/sidebar'
import { AppSidebar } from '@/components/shadcn/layout/app-sidebar'
import { SkipToMain } from '@/components/shadcn/skip-to-main'

type AuthenticatedLayoutProps = {
  children?: React.ReactNode
}

export function AuthenticatedLayout({ children }: AuthenticatedLayoutProps) {
  const defaultOpen = getCookie('sidebar_state') !== 'false'
  return (
    <ThemeProvider>
      <DirectionProvider>
        <SearchProvider>
          <LayoutProvider>
            {/* `shadcn-scope` belongs on the outermost element the panel
                renders: it is what confines shadcn's base layer (see
                styles/shadcn-base.css), and anything outside it that uses a
                bare `border` falls back to currentColor and draws a black
                outline. SidebarInset would cover only the content column. */}
            <SidebarProvider defaultOpen={defaultOpen} className="shadcn-scope">
              <SkipToMain />
              <AppSidebar />
              <SidebarInset
                className={cn(
                  // Set content container, so we can use container queries
                  '@container/content',

                  // If layout is fixed, set the height
                  // to 100svh to prevent overflow
                  'has-data-[layout=fixed]:h-svh',

                  // If layout is fixed and sidebar is inset,
                  // set the height to 100svh - spacing (total margins) to prevent overflow
                  'peer-data-[variant=inset]:has-data-[layout=fixed]:h-[calc(100svh-(var(--spacing)*4))]'
                )}
              >
                {children ?? <Outlet />}
              </SidebarInset>
            </SidebarProvider>
          </LayoutProvider>
        </SearchProvider>
      </DirectionProvider>
    </ThemeProvider>
  )
}
