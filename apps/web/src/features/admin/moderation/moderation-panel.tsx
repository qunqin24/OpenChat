import { Link } from '@tanstack/react-router'
import { useTranslation } from 'react-i18next'

import { Skeleton } from '@/components/shadcn/ui/skeleton'
import { ModerationForm } from './moderation-form'
import { useModerationSettings } from './use-moderation'

/**
 * The 审核 tab of the system settings: the settings form and the way to the
 * log.
 */
export function ModerationPanel() {
  const { t } = useTranslation()
  const settings = useModerationSettings()

  return (
    <div className="space-y-8 py-4">
      <div className="space-y-1">
        <p className="text-muted-foreground text-sm">{t('admin.moderation.intro')}</p>
        <Link to="/admin/moderation-logs" className="text-sm underline underline-offset-4">
          {t('admin.moderation.open-log')}
        </Link>
      </div>

      {settings.isPending ? (
        <Skeleton className="h-96 w-full" />
      ) : settings.isError ? (
        <p className="text-muted-foreground text-sm">{t('state.error')}</p>
      ) : (
        <ModerationForm settings={settings.data} />
      )}
    </div>
  )
}
