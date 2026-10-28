/**
 * The width of the panel beside the chat (a writing block, the sub-agents),
 * as the person dragged it. Per browser, in localStorage: a convenience, not
 * state that must survive - without it the panel takes its default width.
 */
const KEY = 'openchat.panel-width'

/** The narrowest the panel may be, and the least the chat keeps beside it. */
export const PANEL_MIN = 360
export const CHAT_MIN = 420

/** A dragged width kept within what the page allows: null where nothing fits. */
export function clampPanelWidth(width: number, pageWidth: number): number {
  return Math.round(Math.max(PANEL_MIN, Math.min(width, pageWidth - CHAT_MIN)))
}

export function loadPanelWidth(): number | null {
  try {
    const value = Number(localStorage.getItem(KEY))
    return Number.isFinite(value) && value >= PANEL_MIN ? value : null
  } catch {
    return null
  }
}

export function savePanelWidth(width: number | null) {
  try {
    if (width === null) localStorage.removeItem(KEY)
    else localStorage.setItem(KEY, String(width))
  } catch {
    // Private mode or blocked storage: the width lasts this page only.
  }
}
