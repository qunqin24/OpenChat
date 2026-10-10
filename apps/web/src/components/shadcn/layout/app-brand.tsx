import { useTranslation } from 'react-i18next'

import { IconMark } from '@/assets/custom/icon-mark'
import { SidebarMenu, SidebarMenuButton, SidebarMenuItem } from '@/components/shadcn/ui/sidebar'

/**
 * The panel's name at the top of the sidebar.
 *
 * Deviation from upstream: shadcn-admin puts a team switcher here, a dropdown
 * of teams with "add team". OpenChat has no teams, so the menu offered one
 * entry and an action that did nothing; this is the same header row, laid out
 * as the switcher's trigger was — mark, name, subtitle — with no menu behind it.
 * Collapsed to icons, the mark is what remains.
 */
export function AppBrand() {
  const { t } = useTranslation()
  return (
    <SidebarMenu>
      <SidebarMenuItem>
        <SidebarMenuButton size='lg' className='pointer-events-none' asChild>
          <div>
            <div className='flex aspect-square size-8 items-center justify-center rounded-lg bg-sidebar-primary text-sidebar-primary-foreground'>
              <IconMark className='size-4' />
            </div>
            <div className='grid flex-1 text-start text-sm leading-tight'>
              <span className='truncate font-semibold'>OpenChat</span>
              <span className='truncate text-xs'>{t('admin.title')}</span>
            </div>
          </div>
        </SidebarMenuButton>
      </SidebarMenuItem>
    </SidebarMenu>
  )
}
