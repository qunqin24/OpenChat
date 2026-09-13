import { RefreshCw } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/shadcn/ui/button'
import { cn } from '@/lib/utils'
import { useRefreshCatalog } from '../use-catalog'

/**
 * The one control that moves the catalogue. It appears in the toolbar and again
 * in the empty state, because an empty catalogue is the case where refreshing is
 * the only thing left to do.
 */
export function CatalogRefreshButton({
  size = 'sm',
  variant = 'outline',
}: {
  size?: 'sm' | 'default'
  variant?: 'outline' | 'default'
}) {
  const { t } = useTranslation()
  const refresh = useRefreshCatalog()

  return (
    <Button
      variant={variant}
      size={size}
      onClick={() => refresh.mutate()}
      disabled={refresh.isPending}
    >
      {/* Under `prefers-reduced-motion` the icon stops turning, so the label
          carries the state instead: a frozen spinner and a hung page look the
          same. */}
      <RefreshCw className={cn(refresh.isPending && 'animate-spin')} aria-hidden="true" />
      {refresh.isPending ? t('admin.models.refreshing') : t('admin.models.refresh')}
    </Button>
  )
}
