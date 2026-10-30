import AsyncStorage from '@react-native-async-storage/async-storage'
import { Appearance } from 'react-native'
import { create } from 'zustand'
import { createJSONStorage, persist } from 'zustand/middleware'

import i18n, { deviceLanguage, type Language } from './i18n'

export type Theme = 'system' | 'light' | 'dark'
export type LanguageChoice = 'system' | Language

export const themes: Theme[] = ['system', 'light', 'dark']
export const languages: LanguageChoice[] = ['system', 'zh', 'en']

interface AppearanceState {
  theme: Theme
  language: LanguageChoice
  setTheme: (theme: Theme) => void
  setLanguage: (language: LanguageChoice) => void
}

/**
 * How the app looks and speaks, on this device (the web's ui-store theme and
 * language). The theme overrides the system's for the whole app - useColors,
 * the status bar and the system's own dialogs follow `useColorScheme`, which
 * the override sets. Both apply as soon as they are chosen.
 */
export const useAppearance = create<AppearanceState>()(
  persist(
    (set) => ({
      theme: 'system',
      language: 'system',
      setTheme: (theme) => {
        apply(theme, undefined)
        set({ theme })
      },
      setLanguage: (language) => {
        apply(undefined, language)
        set({ language })
      },
    }),
    {
      name: 'openchat.appearance',
      storage: createJSONStorage(() => AsyncStorage),
      partialize: ({ theme, language }) => ({ theme, language }),
      onRehydrateStorage: () => (state) => {
        if (state) apply(state.theme, state.language)
      },
    },
  ),
)

function apply(theme: Theme | undefined, language: LanguageChoice | undefined) {
  if (theme) Appearance.setColorScheme(theme === 'system' ? 'unspecified' : theme)
  if (language) {
    const next = language === 'system' ? deviceLanguage() : language
    if (i18n.language !== next) void i18n.changeLanguage(next)
  }
}
