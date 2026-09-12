import { createApiClient, OpenChatError } from '@openchat/api-client'
import type { ApiError } from '@openchat/api-client'

/** Base URL for every API call; the Vite dev proxy forwards /api to the Go server. */
export const API_BASE_URL = import.meta.env.VITE_API_BASE_URL ?? '/api/v1'

/**
 * `credentials: 'include'` is what makes the browser send and store the
 * HttpOnly session cookie. Without it every request looks signed out.
 */
export const api = createApiClient({ baseUrl: API_BASE_URL, credentials: 'include' })

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
    throw new OpenChatError(result.response.status, result.error as ApiError)
  }
  return result.data as T
}
