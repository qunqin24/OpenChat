import { isOpenChatError } from '@openchat/api-client'

import { queryClient } from '@/lib/query'
import { queryKeys } from '@/lib/query-keys'
import { forgetQueryCache } from '@/lib/query-persist'
import { resetChatStore } from '@/stores/chat-store'

/**
 * The server no longer knows this browser's session: an administrator disabled
 * the account, the password was changed elsewhere, or the cookie simply ran
 * out. Left alone, the open tab goes on half working - conversations read as
 * empty, sends fail with a generic message, nothing says why.
 *
 * The sign-in form's own wrong password is also a 401 (`invalid_credentials`)
 * and is the form's to show.
 */
export function isSessionExpired(error: unknown): boolean {
  return isOpenChatError(error) && error.status === 401 && error.code !== 'invalid_credentials'
}

/** The part of the router this needs; the real one fits. */
export interface SessionRouter {
  latestLocation: { pathname: string; href: string }
  navigate: (options: { to: '/login'; search: { redirect: string }; replace: boolean }) => Promise<unknown>
}

let leaving = false

/**
 * Handles one failure from anywhere: if it is the session ending, everything
 * of this account is dropped (cache, chat state, the stored copy) and the page
 * goes to the sign-in form, which returns to where the person was.
 */
export async function leaveIfSessionExpired(error: unknown, router: SessionRouter): Promise<void> {
  if (!isSessionExpired(error) || leaving) return
  // Only a session this page believed in can end. Signed out already, a 401 is
  // the ordinary answer (the guards redirect on their own), and a page that has
  // yet to learn who it is - the guards' prefetches run beside that check -
  // leaves the verdict to it.
  if (!queryClient.getQueryData(queryKeys.auth.session)) return
  const { pathname, href } = router.latestLocation
  if (pathname === '/login' || pathname === '/register') return

  leaving = true
  try {
    queryClient.clear()
    resetChatStore()
    forgetQueryCache()
    await router.navigate({ to: '/login', search: { redirect: href }, replace: true })
  } finally {
    leaving = false
  }
}
