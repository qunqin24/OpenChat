import { useTranslation } from 'react-i18next'

/** Kept minimal on purpose: export and deletion of one's data come later. */
export function DataTab() {
  const { t } = useTranslation()
  return (
    <div className="n-settings-tab-body" id="tab-data">
      <div className="n-settings-scroll">
        <div className="n-settings-subheading">{t('settings.tabs.data')}</div>
        <p className="n-settings-help">{t('settings.data.soon')} — {t('settings.data.hint')}</p>
        <hr className="n-settings-rule" />
        <p className="n-settings-help">{t('settings.data.temporary')}</p>
      </div>
    </div>
  )
}

export default DataTab
