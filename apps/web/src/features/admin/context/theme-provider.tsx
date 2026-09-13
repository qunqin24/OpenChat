import { createContext, useContext, useEffect, useMemo, useState } from 'react'

import { useUIStore, type Theme } from '@/stores/ui-store'

type ResolvedTheme = Exclude<Theme, 'system'>

type ThemeProviderState = {
  defaultTheme: Theme
  resolvedTheme: ResolvedTheme
  theme: Theme
  setTheme: (theme: Theme) => void
  resetTheme: () => void
}

const ThemeContext = createContext<ThemeProviderState | null>(null)

/**
 * Adapter, not a second source of truth.
 *
 * Upstream keeps the theme in a cookie and writes `light`/`dark` onto <html>
 * itself. The chat UI already does that from the ui-store, and two writers
 * would fight: a change made here would be reverted the next time the store's
 * effect ran. So this keeps upstream's context shape — the ConfigDrawer
 * uses it unchanged — while reading and writing the store.
 */
export function ThemeProvider({ children }: { children: React.ReactNode }) {
  const theme = useUIStore((s) => s.theme)
  const setTheme = useUIStore((s) => s.setTheme)

  // Recomputed on OS changes so `resolvedTheme` stays truthful under 'system'.
  const [systemDark, setSystemDark] = useState(
    () => window.matchMedia('(prefers-color-scheme: dark)').matches,
  )

  useEffect(() => {
    const media = window.matchMedia('(prefers-color-scheme: dark)')
    const onChange = () => setSystemDark(media.matches)
    media.addEventListener('change', onChange)
    return () => media.removeEventListener('change', onChange)
  }, [])

  const value = useMemo<ThemeProviderState>(
    () => ({
      defaultTheme: 'system',
      theme,
      resolvedTheme: theme === 'system' ? (systemDark ? 'dark' : 'light') : theme,
      setTheme,
      resetTheme: () => setTheme('system'),
    }),
    [theme, systemDark, setTheme],
  )

  return <ThemeContext value={value}>{children}</ThemeContext>
}

// eslint-disable-next-line react-refresh/only-export-components
export const useTheme = () => {
  const context = useContext(ThemeContext)
  if (!context) throw new Error('useTheme must be used within a ThemeProvider')
  return context
}
