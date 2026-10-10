import { useSyncExternalStore } from 'react'

import { queryClient } from '@/lib/query'
import { forgetQueryCache } from '@/lib/query-persist'
import { resetChatStore } from '@/stores/chat-store'

/**
 * Whether the page is a guest's. The pages of a guest live under /guest, and
 * everything else is an account's; which it is decides where the API calls go
 * (lib/guest/index.ts) and what the shell shows. It is kept here, outside the
 * router, because the calls are made before the address has changed: a link to
 * /guest from the sign-in page runs the guest page's loaders while the address
 * still says /login.
 *
 * The two modes share query keys on purpose - the same components ask the same
 * questions - so the cache of one must never be seen by the other. Changing
 * mode empties it, and the chat store with it.
 */
export type Mode = 'guest' | 'account'

let mode: Mode | null = null
const listeners = new Set<() => void>()

export const modeOf = (pathname: string): Mode =>
  pathname === '/guest' || pathname.startsWith('/guest/') ? 'guest' : 'account'

/** Called with each address the router is about to show. */
export function syncMode(pathname: string) {
  const next = modeOf(pathname)
  if (next === mode) return
  const first = mode === null
  mode = next
  // The first call is the page loading: an account's cache restored from the
  // last visit is the account's to use, and a guest must not see it.
  if (!first || next === 'guest') {
    queryClient.clear()
    resetChatStore()
  }
  if (next === 'guest') forgetQueryCache()
  listeners.forEach((listener) => listener())
}

export function isGuestMode(): boolean {
  return (mode ?? modeOf(window.location.pathname)) === 'guest'
}

/**
 * The root route's guard, for the address `pathname` that is about to load.
 * Returns whether the load may go on.
 *
 * Only a real navigation changes the mode. The router also runs the guard to
 * preload a link the pointer is over, and nobody has gone anywhere: letting
 * that flip the mode would empty a guest's cache and send their next call to
 * the account API (and back, from the sign-in page's guest button). A preload
 * of the other mode's page is refused outright, since its loaders would fill
 * the shared cache with answers meant for the other mode.
 */
export function enterPath(pathname: string, preload: boolean): boolean {
  if (preload) return modeOf(pathname) === (isGuestMode() ? 'guest' : 'account')
  syncMode(pathname)
  return true
}

const subscribe = (listener: () => void) => {
  listeners.add(listener)
  return () => void listeners.delete(listener)
}

/** For components that show something different to a guest. */
export function useIsGuest(): boolean {
  return useSyncExternalStore(subscribe, isGuestMode, () => false)
}
