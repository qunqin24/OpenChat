import { RefreshCw } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/shadcn/ui/button'
import { cn } from '@/lib/utils'
import { useRefreshCatalog } from '../use-catalog'

/**
 * Fetches models.dev now, rather than waiting for the server's own daily
 * refresh. Beside the catalogue's status line on the model marketplace.
 */
export function CatalogRefreshButton({
  size = 'default',
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
