import type { SessionUser } from '@openchat/api-client'
import type { QueryClient } from '@tanstack/react-query'

import { api, unwrap } from './api'
import { reportError } from './query'
import { queryKeys } from './query-keys'
import { browserTimeZone } from './tasks'

let sent: string | undefined

/**
 * Tells the server the browser's time zone when it is not the one on record:
 * a model scheduling a task for the person reads times in it. Once per page
 * per zone, in the background; a failure is only reported (a 401 signs out).
 */
export function syncTimeZone(queryClient: QueryClient, session: SessionUser) {
  const zone = browserTimeZone()
  if (session.timeZone === zone || sent === zone) return
  sent = zone
  void (async () => {
    try {
      const me = unwrap(await api.PATCH('/auth/me', { body: { timeZone: zone } }))
      queryClient.setQueryData(queryKeys.auth.session, me)
    } catch (error) {
      reportError(error)
    }
  })()
}
