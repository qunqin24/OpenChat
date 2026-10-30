import i18n from 'i18next'
import LanguageDetector from 'i18next-browser-languagedetector'
import { initReactI18next } from 'react-i18next'

import enCommon from '@openchat/chat-core/locales/en/common.json'
import zhCommon from '@openchat/chat-core/locales/zh/common.json'

export const resources = {
  en: { common: enCommon },
  zh: { common: zhCommon },
} as const

export const supportedLanguages = Object.keys(resources) as Array<keyof typeof resources>

void i18n
  .use(LanguageDetector)
  .use(initReactI18next)
  .init({
    resources,
    fallbackLng: 'en',
    supportedLngs: supportedLanguages,
    defaultNS: 'common',
    ns: ['common'],
    interpolation: { escapeValue: false }, // React already escapes
    detection: {
      order: ['localStorage', 'navigator'],
      lookupLocalStorage: 'openchat.lang',
      caches: ['localStorage'],
    },
  })

export default i18n
