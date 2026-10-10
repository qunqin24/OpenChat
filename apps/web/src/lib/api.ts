import { createApiClient, OpenChatError } from '@openchat/api-client'
import type { ApiError } from '@openchat/api-client'

import { API_BASE_URL } from './api-base'
import { isGuestMode } from './guest/mode'

export { API_BASE_URL }

/**
 * The page's fetch. An account's calls go to the network. A guest's are
 * answered in the page where they can be - their conversations are kept in the
 * browser (lib/guest) - and the guest code is loaded only when one is made, so
 * an account never downloads it.
 */
export const appFetch: typeof globalThis.fetch = async (input, init) => {
  if (!isGuestMode()) return globalThis.fetch(input, init)
  const { guestFetch } = await import('./guest')
  return guestFetch(input, init)
}

/**
 * `credentials: 'include'` is what makes the browser send and store the
 * HttpOnly session cookie. Without it every request looks signed out.
 */
export const api = createApiClient({ baseUrl: API_BASE_URL, credentials: 'include', fetch: appFetch })

/**
 * openapi-fetch returns `{ data, error }` instead of throwing. TanStack Query
 * wants thrown errors, so every call goes through here.
 */
export function unwrap<T>(result: {
  data?: T
  error?: unknown
  response: Response
}): T {
  if (result.error !== undefined) {
    throw new OpenChatError(
      result.response.status,
      result.error as ApiError,
      result.response.headers.get('X-Request-Id') ?? undefined,
    )
  }
  return result.data as T
}
