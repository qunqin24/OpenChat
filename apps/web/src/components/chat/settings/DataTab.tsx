import { useNavigate } from '@tanstack/react-router'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { useDeleteAllConversations, useExportConversations } from '@/hooks/use-my-data'
import { useMyStorage } from '@/hooks/use-my-storage'
import { formatSize } from '@openchat/chat-core/attachments'
import { cn } from '@/lib/utils'

/**
 * What one's files take, then taking one's data out and deleting it, as in
 * Open WebUI's Data Controls: a row for each, the title and what it does on
 * the left, the button on the right. Deleting asks once more, in the row,
 * before it does anything.
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

        <StorageMeter />

        <hr className="n-settings-rule" />

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

/** Past this share the meter warns that the room is running out. */
const NEARLY_FULL = 0.9

/**
 * What the person's files take against their quota: a bar in the colours of
 * the kinds of file (as their badges), each kind's share under it, and the way
 * to the library to make room. Full, it says uploads are refused until then.
 */
function StorageMeter() {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const storage = useMyStorage()
  const data = storage.data
  const share = data && data.quotaBytes > 0 ? data.usedBytes / data.quotaBytes : data && data.usedBytes > 0 ? 1 : 0

  return (
    <div className="n-settings-storage">
      <div className="n-settings-data-row">
        <div className="n-settings-data-text">
          <div className="n-settings-subheading">{t('settings.data.storage-title')}</div>
          <p className="n-settings-help">
            {data
              ? t('settings.data.storage-used', { used: formatSize(data.usedBytes), quota: formatSize(data.quotaBytes) })
              : storage.isError
                ? t('settings.data.storage-failed')
                : t('state.loading')}
          </p>
        </div>
        <button type="button" className="n-settings-outline" onClick={() => void navigate({ to: '/library' })}>
          {t('settings.data.storage-manage')}
        </button>
      </div>
      {data && (
        <>
          <div
            className={cn('n-storage-bar', share >= 1 ? 'is-full' : share >= NEARLY_FULL && 'is-nearly-full')}
            role="meter"
            aria-label={t('settings.data.storage-title')}
            aria-valuemin={0}
            aria-valuemax={data.quotaBytes}
            aria-valuenow={Math.min(data.usedBytes, data.quotaBytes)}
          >
            {data.kinds.map((kind) => (
              <span
                key={kind.kind}
                className={`n-storage-part is-${kind.kind}`}
                style={{ width: `${Math.min(100, (kind.bytes / Math.max(data.quotaBytes, data.usedBytes, 1)) * 100)}%` }}
              />
            ))}
          </div>
          {data.kinds.length > 0 && (
            <ul className="n-storage-legend">
              {data.kinds.map((kind) => (
                <li key={kind.kind}>
                  <span className={`n-storage-dot is-${kind.kind}`} aria-hidden="true" />
                  {t(`settings.data.storage-kind.${kind.kind}`)}
                  <span className="n-storage-amount">{formatSize(kind.bytes)}</span>
                </li>
              ))}
            </ul>
          )}
          {share >= 1 ? (
            <p className="n-settings-help is-warning">{t('settings.data.storage-full')}</p>
          ) : share >= NEARLY_FULL ? (
            <p className="n-settings-help is-warning">{t('settings.data.storage-nearly-full')}</p>
          ) : null}
        </>
      )}
    </div>
  )
}

export default DataTab
