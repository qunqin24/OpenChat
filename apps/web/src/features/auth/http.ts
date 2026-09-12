import { api } from '@/lib/api'
import { AuthFieldError, type AuthApi, type AuthErrorKey, type SessionUser } from './types'

/**
 * Auth against HttpOnly session cookies. The client sends `credentials:
 * 'include'` on every request (see lib/api.ts) — without it the browser
 * silently drops the cookie and every call looks signed out.
 */

/**
 * Server error codes mapped to locale keys. The server's own `message` is never
 * shown: it is not localised, and treating it as a translation key would render
 * the raw string whenever the two drift apart.
 */
const SERVER_ERRORS: Record<string, AuthErrorKey> = {
  invalid_credentials: 'auth.error.invalid-credentials',
  email_taken: 'auth.error.email-taken',
  email_required: 'auth.error.email-required',
  name_required: 'auth.error.name-required',
  password_short: 'auth.error.password-short',
  account_disabled: 'auth.error.account-disabled',
}

interface ServerError {
  code?: string
  field?: AuthFieldError['field']
}

function toAuthError(error: unknown): AuthFieldError {
  const body = (error ?? {}) as ServerError
  return new AuthFieldError(
    body.field ?? 'password',
    SERVER_ERRORS[body.code ?? ''] ?? 'auth.error.unknown',
  )
}

export function createHttpAuthApi(): AuthApi {
  return {
    async session() {
      const { data, error, response } = await api.GET('/auth/session')
      // 401 is the ordinary "not signed in" answer, not a failure.
      if (response.status === 401) return null
      if (error) throw toAuthError(error)
      return data as SessionUser
    },

    async login(input) {
      const { data, error } = await api.POST('/auth/login', { body: input })
      if (error) throw toAuthError(error)
      return data as SessionUser
    },

    async register(input) {
      const { data, error } = await api.POST('/auth/register', { body: input })
      if (error) throw toAuthError(error)
      return data as SessionUser
    },

    async logout() {
      const { error } = await api.POST('/auth/logout')
      if (error) throw toAuthError(error)
    },
  }
}
