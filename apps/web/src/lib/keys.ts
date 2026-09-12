import type { KeyboardEvent } from 'react'

/** Enter, but not while an IME candidate window is open. */
export function isEnter(event: KeyboardEvent): boolean {
  return event.key === 'Enter' && !event.nativeEvent.isComposing
}

export function withCtrl(event: KeyboardEvent): boolean {
  return event.ctrlKey || event.metaKey
}

export function withShift(event: KeyboardEvent): boolean {
  return event.shiftKey
}
