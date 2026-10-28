import { createApiClient, OpenChatError } from '@openchat/api-client'
import type { ApiError } from '@openchat/api-client'

import { API_BASE_URL } from './api-base'
import { noteBuild } from './build-check'

export { API_BASE_URL }

/** The page's fetch: the network's, with every answer checked for a newer build. */
export const appFetch: typeof globalThis.fetch = async (input, init) => {
  const response = await globalThis.fetch(input, init)
  noteBuild(response)
  return response
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
