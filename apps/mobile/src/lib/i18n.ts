import { getLocales } from 'expo-localization'
import i18n from 'i18next'
import { initReactI18next } from 'react-i18next'

import enCommon from '@openchat/chat-core/locales/en/common.json'
import zhCommon from '@openchat/chat-core/locales/zh/common.json'

// The web app's strings; the app's own live under `mobile.*` in the same
// files, so one parity test covers both.
export const resources = {
  en: { common: enCommon },
  zh: { common: zhCommon },
} as const

export type Language = keyof typeof resources

/** The device's first language we have, else English. */
function deviceLanguage(): Language {
  for (const locale of getLocales()) {
    if (locale.languageCode && locale.languageCode in resources) return locale.languageCode as Language
  }
  return 'en'
}

void i18n.use(initReactI18next).init({
  resources,
  lng: deviceLanguage(),
  fallbackLng: 'en',
  defaultNS: 'common',
  ns: ['common'],
  interpolation: { escapeValue: false },
})

export default i18n
