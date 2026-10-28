import { useNavigate } from '@tanstack/react-router'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { useDeleteAllConversations, useExportConversations } from '@/hooks/use-my-data'

/**
 * Taking one's data out and deleting it, as in Open WebUI's Data Controls: a
 * row for each, the title and what it does on the left, the button on the
 * right. Deleting asks once more, in the row, before it does anything.
 */
export function DataTab() {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const exportAll = useExportConversations()
  const deleteAll = useDeleteAllConversations()
  const [confirming, setConfirming] = useState(false)

  const download = () =>
    exportAll.mutate(undefined, {
      onSuccess: () => toast.success(t('settings.data.export-done')),
      onError: () => toast.error(t('settings.data.export-failed')),
    })

  const remove = () =>
    deleteAll.mutate(undefined, {
      onSuccess: (result) => {
        setConfirming(false)
        toast.success(t('settings.data.deleted', { count: result.deleted }))
        // The chat on screen, if any, is one of the deleted: start a new one,
        // with the dialog still open on this tab.
        void navigate({ to: '/', search: { settings: 'data' } as never, replace: true })
      },
      onError: () => {
        setConfirming(false)
        toast.error(t('settings.data.delete-failed'))
      },
    })

  return (
    <div className="n-settings-tab-body" id="tab-data">
      <div className="n-settings-scroll">
        <div className="n-settings-heading-block">
          <div className="n-settings-heading">{t('settings.tabs.data')}</div>
        </div>

        <div className="n-settings-data-row">
          <div className="n-settings-data-text">
            <div className="n-settings-subheading">{t('settings.data.export-title')}</div>
            <p className="n-settings-help">{t('settings.data.export-desc')}</p>
          </div>
          <button type="button" className="n-settings-outline" disabled={exportAll.isPending} onClick={download}>
            {t(exportAll.isPending ? 'settings.data.exporting' : 'settings.data.export-button')}
          </button>
        </div>

        <hr className="n-settings-rule" />

        <div className="n-settings-data-row">
          <div className="n-settings-data-text">
            <div className="n-settings-subheading">{t('settings.data.delete-title')}</div>
            <p className="n-settings-help">{t('settings.data.delete-desc')}</p>
          </div>
          {confirming ? null : (
            <button type="button" className="n-settings-outline is-danger" onClick={() => setConfirming(true)}>
              {t('settings.data.delete-button')}
            </button>
          )}
        </div>
        {confirming && (
          <div className="n-settings-confirm" role="alertdialog" aria-label={t('settings.data.delete-title')}>
            <span>{t('settings.data.delete-confirm')}</span>
            <button type="button" className="n-settings-danger" disabled={deleteAll.isPending} onClick={remove}>
              {t('settings.data.delete-yes')}
            </button>
            <button type="button" className="n-settings-soft" disabled={deleteAll.isPending} onClick={() => setConfirming(false)}>
              {t('settings.cancel')}
            </button>
          </div>
        )}

        <hr className="n-settings-rule" />
        <p className="n-settings-help">{t('settings.data.temporary')}</p>
      </div>
    </div>
  )
}

export default DataTab
