import { create } from 'zustand'
import { persist } from 'zustand/middleware'

export type Theme = 'light' | 'dark' | 'system'

export const SIDEBAR_WIDTH = { default: 245, min: 220, max: 480 } as const

interface UIState {
  theme: Theme
  sidebarOpen: boolean
  sidebarWidth: number
  sidebarChatsOpen: boolean
  setTheme: (theme: Theme) => void
  toggleSidebar: () => void
  setSidebarOpen: (open: boolean) => void
  setSidebarWidth: (width: number) => void
  setSidebarChatsOpen: (open: boolean) => void
}

export const useUIStore = create<UIState>()(
  persist(
    (set) => ({
      theme: 'system',
      sidebarOpen: true,
      sidebarWidth: SIDEBAR_WIDTH.default,
      sidebarChatsOpen: true,
      setTheme: (theme) => set({ theme }),
      toggleSidebar: () => set((state) => ({ sidebarOpen: !state.sidebarOpen })),
      setSidebarOpen: (sidebarOpen) => set({ sidebarOpen }),
      setSidebarWidth: (width) => {
        if (!Number.isFinite(width)) return
        set({ sidebarWidth: Math.min(SIDEBAR_WIDTH.max, Math.max(SIDEBAR_WIDTH.min, width)) })
      },
      setSidebarChatsOpen: (sidebarChatsOpen) => set({ sidebarChatsOpen }),
    }),
    { name: 'openchat.ui' },
  ),
)

/**
 * Applies `theme` to <html>. Returns a cleanup for the system-preference
 * listener, so callers can use it straight from an effect.
 */
export function applyTheme(theme: Theme): () => void {
  const media = window.matchMedia('(prefers-color-scheme: dark)')

  const paint = () => {
    const dark = theme === 'dark' || (theme === 'system' && media.matches)
    document.documentElement.classList.toggle('dark', dark)
  }

  paint()
  if (theme !== 'system') return () => undefined

  media.addEventListener('change', paint)
  return () => media.removeEventListener('change', paint)
}
