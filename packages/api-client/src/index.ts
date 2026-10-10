import createClient from 'openapi-fetch'

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
