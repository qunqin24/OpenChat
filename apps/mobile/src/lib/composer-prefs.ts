import AsyncStorage from '@react-native-async-storage/async-storage'
import type { ReasoningEffort } from '@openchat/api-client'
import { create } from 'zustand'
import { createJSONStorage, persist } from 'zustand/middleware'

/**
 * The composer's last choices - model, thinking effort, web search - so a new
 * chat starts the way the previous one was set (the web's lib/composer-prefs.ts).
 * Per account, on this device: a convenience; lost, it means the defaults.
 */
export interface ComposerPrefs {
  model?: string
  effort?: ReasoningEffort
  webSearch?: boolean
}

interface PrefsState {
  byUser: Record<string, ComposerPrefs>
  save: (userId: string, change: ComposerPrefs) => void
}

export const useComposerPrefs = create<PrefsState>()(
  persist(
    (set) => ({
      byUser: {},
      save: (userId, change) => set((state) => ({ byUser: { ...state.byUser, [userId]: { ...state.byUser[userId], ...change } } })),
    }),
    { name: 'openchat.composer', storage: createJSONStorage(() => AsyncStorage), partialize: (state) => ({ byUser: state.byUser }) },
  ),
)
