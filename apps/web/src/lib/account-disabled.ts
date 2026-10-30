import { create } from 'zustand'

import { accountDisabledOf, type AccountDisabled } from '@openchat/chat-core/account-disabled'

// Reading a 403 account_disabled is chat-core's (account-disabled); keeping
// what it said and taking the page to /disabled is the web app's.

interface AccountDisabledState {
  /** What the server said last, for /disabled to show; null until it has. */
  details: AccountDisabled | null
  set: (details: AccountDisabled | null) => void
}

export const useAccountDisabled = create<AccountDisabledState>((set) => ({
  details: null,
  set: (details) => set({ details }),
}))

/** The part of the router this needs; the real one fits. */
export interface DisabledRouter {
  latestLocation: { pathname: string }
  navigate: (options: { to: '/disabled'; replace: boolean }) => Promise<unknown>
}

/**
 * Handles one failure from anywhere: if it says the account is disabled, keeps
 * why and takes the page to /disabled. The sign-in forms show their own.
 */
export async function showIfAccountDisabled(error: unknown, router: DisabledRouter): Promise<void> {
  const details = accountDisabledOf(error)
  if (!details) return
  useAccountDisabled.getState().set(details)
  const { pathname } = router.latestLocation
  if (pathname === '/disabled' || pathname === '/login' || pathname === '/register') return
  await router.navigate({ to: '/disabled', replace: true })
}
