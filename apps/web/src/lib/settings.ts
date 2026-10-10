import { useNavigate, useSearch } from '@tanstack/react-router'
import { useCallback } from 'react'

/** The tabs of the settings dialog, in the order they are listed. */
export const SETTINGS_TABS = ['general', 'personalization', 'account', 'data'] as const
export type SettingsTab = (typeof SETTINGS_TABS)[number]

export function isSettingsTab(value: unknown): value is SettingsTab {
  return SETTINGS_TABS.some((tab) => tab === value)
}

/**
 * The settings dialog lives in the address (`?settings=personalization`), so a
 * tab can be linked to — the memory chip in a reply does — and the browser's
 * Back button closes the dialog, which on a phone is what people reach for.
 * Opening pushes an entry; moving between tabs replaces it.
 */
export function useSettings() {
  const navigate = useNavigate()
  const { settings } = useSearch({ strict: false }) as { settings?: SettingsTab }
  const tab = isSettingsTab(settings) ? settings : undefined

  const go = useCallback(
    (next: SettingsTab | undefined, replace: boolean) =>
      navigate({ to: '.', search: ((prev: Record<string, unknown>) => ({ ...prev, settings: next })) as never, replace }),
    [navigate],
  )
  const open = useCallback((next: SettingsTab = 'general') => void go(next, false), [go])
  const select = useCallback((next: SettingsTab) => void go(next, true), [go])
  const close = useCallback(() => void go(undefined, false), [go])
  return { tab, open, select, close }
}
