import { useSyncExternalStore } from 'react'

/**
 * Whether the guest's conversations are being kept in the browser (IndexedDB)
 * or only in memory because the browser would not give them a place
 * (open-storage.ts). Unknown until guest mode has started; kept apart from the
 * rest of the guest code so the notice that shows it costs an account nothing.
 */
let kept: boolean | null = null
const listeners = new Set<() => void>()

export function setStorageKept(value: boolean) {
  if (kept === value) return
  kept = value
  listeners.forEach((listener) => listener())
}

const subscribe = (listener: () => void) => {
  listeners.add(listener)
  return () => void listeners.delete(listener)
}

/** True only once it is known that nothing will survive a reload. */
export function useStorageLost(): boolean {
  return useSyncExternalStore(
    subscribe,
    () => kept === false,
    () => false,
  )
}
