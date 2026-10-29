import { Check, ChevronDown } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from '@/components/ui/dropdown-menu'
import { supportedLanguages } from '@/lib/i18n'
import { useUIStore, type Theme } from '@/stores/ui-store'

const THEMES: Theme[] = ['light', 'dark', 'system']
const LANGUAGE_LABELS = { en: 'English', zh: '中文' }

/**
 * Theme and language, which the account menu also keeps as quick toggles.
 * Both are classic rows, a small label on the left and the control on the
 * right: the theme as three small cards, each a drawing of the app in that
 * theme on a backdrop of its light, and the language as a button that is only
 * text, opening the chat's own menu (as the account menu's language list). Both apply as soon as they are changed, so the tab has no save button.
 */
export function GeneralTab() {
  const { t, i18n } = useTranslation()
  const theme = useUIStore((s) => s.theme)
  const setTheme = useUIStore((s) => s.setTheme)

  return (
    <div className="n-settings-tab-body" id="tab-general">
      <div className="n-settings-scroll">
        <div className="n-settings-subheading">{t('settings.general.section')}</div>
        <div className="n-settings-line n-theme-line">
          <span className="n-settings-small-label" id="settings-theme">{t('settings.general.theme')}</span>
          <div className="n-theme-options" role="radiogroup" aria-labelledby="settings-theme">
            {THEMES.map((value) => (
              <label key={value} className="n-theme-option">
                <input
                  type="radio"
                  name="settings-theme"
                  className="n-theme-input"
                  value={value}
                  checked={theme === value}
                  onChange={() => setTheme(value)}
                />
                <span className={`n-theme-card is-${value}`} aria-hidden="true">
                  {value === 'system' ? (
                    <span className="n-theme-window-pair">
                      <ThemeWindow dark />
                      <ThemeWindow className="is-right-half" />
                    </span>
                  ) : (
                    <ThemeWindow dark={value === 'dark'} />
                  )}
                </span>
                <span className="n-theme-name">{t(`chat.theme.${value}`)}</span>
              </label>
            ))}
          </div>
        </div>
        <div className="n-settings-line">
          <span className="n-settings-small-label" id="settings-language">{t('settings.general.language')}</span>
          <DropdownMenu>
            <DropdownMenuTrigger className="n-settings-menu-button" aria-labelledby="settings-language settings-language-value">
              <span id="settings-language-value">{LANGUAGE_LABELS[i18n.resolvedLanguage as keyof typeof LANGUAGE_LABELS]}</span>
              <ChevronDown aria-hidden="true" />
            </DropdownMenuTrigger>
            {/* Over the settings dialog, which stands above the page's menus. */}
            <DropdownMenuContent className="n-menu-over-dialog" align="end">
              {supportedLanguages.map((lang) => (
                <DropdownMenuItem key={lang} onSelect={() => void i18n.changeLanguage(lang)}>
                  <span>{LANGUAGE_LABELS[lang]}</span>
                  {i18n.resolvedLanguage === lang && <Check className="n-menu-check" aria-hidden="true" />}
                </DropdownMenuItem>
              ))}
            </DropdownMenuContent>
          </DropdownMenu>
        </div>
      </div>
    </div>
  )
}

/**
 * The app drawn small in one theme: a sidebar of a few lines, a question on the
 * right, an answer of three lines, and the composer with its send button. The
 * system card lays a light one over a dark one and shows its right half.
 */
function ThemeWindow({ dark = false, className = '' }: { dark?: boolean; className?: string }) {
  return (
    <span className={`n-theme-window${dark ? ' is-dark' : ''} ${className}`}>
      <span className="n-theme-window-side"><i /><i /><i /><i /></span>
      <span className="n-theme-window-main">
        <span className="n-theme-window-question" />
        <span className="n-theme-window-answer"><i /><i /><i /></span>
        <span className="n-theme-window-composer"><b /></span>
      </span>
    </span>
  )
}

export default GeneralTab
