import { useTranslation } from 'react-i18next'

/** What a list says when the window held nothing for it. */
export function Empty({ message }: { message?: string }) {
  const { t } = useTranslation()
  return (
    <div className="text-muted-foreground flex h-32 items-center justify-center px-6 text-center text-sm">
      {message ?? t('admin.dashboard.empty')}
    </div>
  )
}
