import { useTranslation } from 'react-i18next'

import { supportedLanguages } from '@/lib/i18n'
import { useUIStore, type Theme } from '@/stores/ui-store'

const THEMES: Theme[] = ['system', 'light', 'dark']
const LANGUAGE_LABELS = { en: 'English', zh: '中文' }

/**
 * Theme and language, which the account menu also keeps as quick toggles.
 * Rows are the classic ones: a small label on the left and, on the right, a
 * select that is only text. They apply as soon as they are changed, so the tab
 * has no save button.
 */
export function GeneralTab() {
  const { t, i18n } = useTranslation()
  const theme = useUIStore((s) => s.theme)
  const setTheme = useUIStore((s) => s.setTheme)

  return (
    <div className="n-settings-tab-body" id="tab-general">
      <div className="n-settings-scroll">
        <div className="n-settings-subheading">{t('settings.general.section')}</div>
        <div className="n-settings-line">
          <label className="n-settings-small-label" htmlFor="settings-theme">{t('settings.general.theme')}</label>
          <select
            id="settings-theme"
            className="n-settings-select"
            value={theme}
            onChange={(e) => setTheme(e.target.value as Theme)}
          >
            {THEMES.map((value) => (
              <option key={value} value={value}>{t(`next.theme.${value}`)}</option>
            ))}
          </select>
        </div>
        <div className="n-settings-line">
          <label className="n-settings-small-label" htmlFor="settings-language">{t('settings.general.language')}</label>
          <select
            id="settings-language"
            className="n-settings-select"
            value={i18n.resolvedLanguage}
            onChange={(e) => void i18n.changeLanguage(e.target.value)}
          >
            {supportedLanguages.map((lang) => (
              <option key={lang} value={lang}>{LANGUAGE_LABELS[lang]}</option>
            ))}
          </select>
        </div>
      </div>
    </div>
  )
}

export default GeneralTab
