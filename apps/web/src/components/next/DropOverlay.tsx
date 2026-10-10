import { useTranslation } from 'react-i18next'

/**
 * FilesOverlay / AddFilesPlaceholder from OpenWebUI, scoped to the chat pane.
 * `unavailable` says why a drop would be refused, in place of inviting one.
 */
export function DropOverlay({ show, unavailable }: { show: boolean; unavailable?: string }) {
  const { t } = useTranslation()
  if (!show) return null

  return (
    <div className="n-files-overlay" role="status" aria-live="polite">
      <div className="n-files-overlay-copy">
        <h2>{t('next.file.add')}</h2>
        <p>{unavailable ?? t('next.file.drop')}</p>
      </div>
    </div>
  )
}
