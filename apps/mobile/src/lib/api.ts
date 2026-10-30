import { createApiClient } from '@openchat/api-client'

import { serverUrl } from '@/config'
import i18n from './i18n'
import { currentToken } from './token'

export { unwrap } from '@openchat/api-client'

/**
 * The typed client. Every request carries the session as a bearer token (an
 * app has no cookie jar the server could use) and the language the server
 * should answer in. React Native's fetch sends no Origin, which is what lets
 * the server hand this client a token at all (`X-OpenChat-Client: app`).
 */
export const api = createApiClient({ baseUrl: `${serverUrl}/api/v1` })

/** The header that asks a sign-in for a token instead of a cookie. */
export const asApp = { header: { 'X-OpenChat-Client': 'app' } } as const

let onUnauthorized: (() => void) | undefined

/** Called when the server stops recognising the token (signed out elsewhere, expired). */
export function setUnauthorizedListener(listener: () => void) {
  onUnauthorized = listener
}

api.use({
  onRequest({ request }) {
    const token = currentToken()
    if (token) request.headers.set('Authorization', `Bearer ${token}`)
    request.headers.set('Accept-Language', i18n.language)
    return request
  },
  onResponse({ request, response }) {
    // A 401 to a request that carried the token means the session is gone. A
    // sign-in's own 401 (wrong password) carries none and is the form's.
    // Returns nothing: openapi-fetch takes any returned value as a replacement
    // and, under React Native, refuses its own Response handed back.
    if (response.status === 401 && request.headers.has('Authorization')) onUnauthorized?.()
  },
})
