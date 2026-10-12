import { useSyncExternalStore } from 'react'

/**
 * Whether the page is in dark mode now: the `.dark` class applyTheme puts on
 * <html> (stores/ui-store.ts), followed as it changes. For what cannot take the
 * `--n-*` colours from CSS and has to be told a theme, such as a Mermaid diagram.
 */
export function useDark(): boolean {
  return useSyncExternalStore(subscribe, isDark, () => false)
}

const isDark = () => document.documentElement.classList.contains('dark')

function subscribe(listener: () => void) {
  const observer = new MutationObserver(listener)
  observer.observe(document.documentElement, { attributes: true, attributeFilter: ['class'] })
  return () => observer.disconnect()
}
