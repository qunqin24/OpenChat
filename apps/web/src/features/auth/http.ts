import { API_BASE_URL } from '@/lib/api'
import { AuthFieldError, type AuthApi, type AuthErrorKey, type SessionUser } from './types'

/**
 * The real implementation, against HttpOnly session cookies.
 *
 * NOT YET EXERCISED: the backend has no auth endpoints, so this is the shape
 * the contract is expected to take, not verified code. Switch to it in
 * `features/auth/index.ts` once `api/openapi.yaml` grows the routes below.
 *
 * `credentials: 'include'` is the whole point — it is what makes the browser
 * send and store the session cookie. Without it the cookie is silently dropped.
 */

interface ErrorBody {
  code?: string
  field?: AuthFieldError['field']
}

/**
 * Server error codes mapped to locale keys. The server's own `message` is
 * never shown: it is not localised, and treating it as a translation key would
 * render the raw string whenever the two drift apart.
 */
const SERVER_ERRORS: Record<string, AuthErrorKey> = {
  invalid_credentials: 'auth.error.invalid-credentials',
  email_taken: 'auth.error.email-taken',
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const response = await fetch(`${API_BASE_URL}${path}`, {
    ...init,
    credentials: 'include',
    headers: { 'Content-Type': 'application/json', ...init?.headers },
  })

  if (!response.ok) {
    let body: ErrorBody = {}
    try {
      body = (await response.json()) as ErrorBody
    } catch {
      // Non-JSON error body; fall back to a generic message.
    }
    const key = SERVER_ERRORS[body.code ?? ''] ?? 'auth.error.unknown'
    throw new AuthFieldError(body.field ?? 'password', key)
  }

  return response.status === 204 ? (undefined as T) : ((await response.json()) as T)
}

export function createHttpAuthApi(): AuthApi {
  return {
    async session() {
      const response = await fetch(`${API_BASE_URL}/auth/session`, { credentials: 'include' })
      // 401 is the ordinary "not signed in" answer, not a failure.
      if (response.status === 401) return null
      if (!response.ok) throw new Error(`auth: session check failed (${response.status})`)
      return (await response.json()) as SessionUser
    },

    login: (input) =>
      request<SessionUser>('/auth/login', { method: 'POST', body: JSON.stringify(input) }),

    register: (input) =>
      request<SessionUser>('/auth/register', { method: 'POST', body: JSON.stringify(input) }),

    logout: () => request<void>('/auth/logout', { method: 'POST' }),
  }
}
