import { LoaderCircle } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Toaster as Sonner, type ToasterProps } from 'sonner'

import { useUIStore } from '@/stores/ui-store'

// OpenWebUI's svelte-sonner 0.3.28 offsets, including safe-area padding.
// React sonner defaults to 24px on desktop and 16px on every mobile edge.
const OFFSET: NonNullable<ToasterProps['offset']> = {
  top: 'max(32px, env(safe-area-inset-top))',
  right: 'max(32px, env(safe-area-inset-right))',
  bottom: 'max(32px, env(safe-area-inset-bottom))',
  left: 'max(32px, env(safe-area-inset-left))',
}

const MOBILE_OFFSET: NonNullable<ToasterProps['mobileOffset']> = {
  top: 'max(20px, env(safe-area-inset-top))',
  right: 'max(16px, env(safe-area-inset-right))',
  bottom: 'max(20px, env(safe-area-inset-bottom))',
  left: 'max(16px, env(safe-area-inset-left))',
}

/**
 * One app-level notification surface, mounted outside route-specific shells.
 * Its own palette matches OpenWebUI (+layout.svelte at 0a7c15832f); it does
 * not inherit the admin vendor toaster's shadcn palette.
 */
export function Toaster() {
  const theme = useUIStore((state) => state.theme)
  const { t } = useTranslation()

  return (
    <Sonner
      className="n-toast"
      theme={theme}
      position="top-right"
      richColors
      closeButton
      duration={4000}
      gap={14}
      visibleToasts={3}
      expand={false}
      offset={OFFSET}
      mobileOffset={MOBILE_OFFSET}
      containerAriaLabel={t('notifications.region')}
      toastOptions={{ closeButtonAriaLabel: t('notifications.close') }}
      icons={{
        loading: (
          <span className="n-toast-spinner" data-motion="essential" aria-hidden="true">
            <LoaderCircle size={16} />
          </span>
        ),
      }}
    />
  )
}
