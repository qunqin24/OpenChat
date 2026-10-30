import { create } from 'zustand'

import type { LocalFile } from './upload'

/** What another app shared into OpenChat, waiting for a new chat to take it. */
export interface Shared {
  text: string
  files: (LocalFile & { width?: number; height?: number })[]
}

/**
 * The share between arriving (ShareInbox, which opens a new chat for it) and
 * the new chat's composer taking it: its words in the box, its files in the
 * tray - once the models are known, since what can be attached depends on
 * the model.
 */
export const useShared = create<{ shared: Shared | null; put: (shared: Shared) => void; take: () => Shared | null }>()((set, get) => ({
  shared: null,
  put: (shared) => set({ shared }),
  take: () => {
    const shared = get().shared
    set({ shared: null })
    return shared
  },
}))
