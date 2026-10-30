import createClient from 'openapi-fetch'

import { OpenChatError } from './errors'
import type { ApiError } from './types'

import type { paths } from './schema'

export * from './errors'
export * from './genui'
export * from './stream'
export * from './types'
export type { paths, components } from './schema'

export interface ClientOptions {
  /** Base URL of the API, e.g. `/api/v1` or `https://api.example.com/api/v1`. */
  baseUrl: string
  /** Extra headers applied to every request (locale, tracing, …). */
  headers?: Record<string, string>
  /** Pass 'include' so the browser carries the HttpOnly session cookie. */
  credentials?: RequestCredentials
  fetch?: typeof globalThis.fetch
}

/** A fully typed client generated from api/openapi.yaml. */
export function createApiClient(options: ClientOptions) {
  return createClient<paths>({
    baseUrl: options.baseUrl,
    headers: options.headers,
    credentials: options.credentials,
    fetch: options.fetch,
  })
}

export type ApiClient = ReturnType<typeof createApiClient>

/**
 * openapi-fetch returns `{ data, error }` instead of throwing. TanStack Query
 * wants thrown errors, so every call goes through here: the error as an
 * OpenChatError carrying the request id the server logged it under.
 */
export function unwrap<T>(result: { data?: T; error?: unknown; response: Response }): T {
  if (result.error !== undefined) {
    throw new OpenChatError(
      result.response.status,
      result.error as ApiError,
      result.response.headers.get('X-Request-Id') ?? undefined,
    )
  }
  return result.data as T
}
