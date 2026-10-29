import { startAuthentication, WebAuthnError } from '@simplewebauthn/browser'

import { disabledDetails } from '@/lib/account-disabled'
import { api, unwrap } from '@/lib/api'
import {
  AuthFieldError,
  type AdminContact,
  type AuthApi,
  type AuthErrorKey,
  type SessionUser,
} from './types'

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
  name_taken: 'auth.error.name-taken',
  email_required: 'auth.error.email-required',
  name_required: 'auth.error.name-required',
  name_invalid: 'auth.error.name-invalid',
  password_short: 'auth.error.password-short',
  account_disabled: 'auth.error.account-disabled',
  pending_approval: 'auth.error.pending-approval',
  registration_closed: 'auth.error.registration-closed',
  login_rate_limited: 'auth.error.login-rate-limited',
  register_rate_limited: 'auth.error.register-rate-limited',
  nothing_to_update: 'auth.error.nothing-to-update',
  invalid_passkey: 'auth.error.invalid-passkey',
}

interface ServerError {
  code?: string
  field?: AuthFieldError['field']
  details?: { admin?: unknown }
}

/** details.admin when it has the shape of an AdminContact; anything else is no contact. */
function adminOf(value: unknown): AdminContact | undefined {
  if (!value || typeof value !== 'object') return undefined
  const { name, email } = value as Record<string, unknown>
  return typeof name === 'string' && typeof email === 'string' ? { name, email } : undefined
}

/** Failures of the whole attempt rather than of one input: shown as the form's banner. */
const FORM_LEVEL = new Set([
  'invalid_passkey',
  'pending_approval',
  'registration_closed',
  'login_rate_limited',
  'register_rate_limited',
])

function toAuthError(error: unknown): AuthFieldError {
  const body = (error ?? {}) as ServerError
  const field = FORM_LEVEL.has(body.code ?? '') ? 'form' : (body.field ?? 'password')

  // The server reuses `invalid_credentials` for "that is not your current
  // password", which reads wrong on a settings form; the field it blames is
  // what tells the two apart.
  const key: AuthErrorKey =
    body.code === 'invalid_credentials' && field === 'currentPassword'
      ? 'auth.error.password-wrong'
      : (SERVER_ERRORS[body.code ?? ''] ?? 'auth.error.unknown')

  const disabled = body.code === 'account_disabled' ? disabledDetails(body.details) : undefined
  return new AuthFieldError(field, key, adminOf(body.details?.admin), disabled)
}

export function createHttpAuthApi(): AuthApi {
  return {
    async session() {
      const result = await api.GET('/auth/session')
      const { data, error, response } = result
      // 401 is the ordinary "not signed in" answer, not a failure.
      if (response.status === 401) return null
      // A disabled account's session is kept and says why: thrown as it came,
      // for the guard to take the page to /disabled (lib/account-disabled.ts).
      if (response.status === 403 && (error as ServerError | undefined)?.code === 'account_disabled') unwrap(result)
      if (error) throw toAuthError(error)
      return data as SessionUser
    },

    async login(input) {
      const { data, error } = await api.POST('/auth/login', { body: input })
      if (error) throw toAuthError(error)
      return data as SessionUser
    },

    async register(input) {
      const { data, error, response } = await api.POST('/auth/register', { body: input })
      if (error) throw toAuthError(error)
      // 202: created, but an administrator has to approve it first. No session.
      if (response.status === 202) {
        return { status: 'pending', admin: adminOf((data as { admin?: unknown } | undefined)?.admin) }
      }
      return { status: 'signed_in', user: data as SessionUser }
    },

    async logout() {
      const { error } = await api.POST('/auth/logout')
      if (error) throw toAuthError(error)
    },

    async updateProfile(input) {
      const { data, error } = await api.PATCH('/auth/me', { body: input })
      if (error) throw toAuthError(error)
      return data as SessionUser
    },

    async changePassword(input) {
      const { error } = await api.POST('/auth/password', { body: input })
      if (error) throw toAuthError(error)
    },

    async passkeyLogin() {
      const begun = await api.POST('/auth/passkey/options')
      if (begun.error || !begun.data) throw toAuthError(begun.error)
      let credential
      try {
        credential = await startAuthentication({ optionsJSON: begun.data.publicKey as never })
      } catch (error) {
        if (passkeyDeclined(error)) return null
        throw new AuthFieldError('form', unsupported(error) ? 'auth.error.passkey-unsupported' : 'auth.error.invalid-passkey')
      }
      const { data, error } = await api.POST('/auth/passkey', {
        body: { challengeId: begun.data.challengeId, credential: credential as unknown as Record<string, unknown> },
      })
      if (error) throw toAuthError(error)
      return data as SessionUser
    },
  }
}

/** The person closed the browser's passkey prompt, or let it time out: nothing to report. */
export function passkeyDeclined(error: unknown): boolean {
  const name = error instanceof WebAuthnError ? (error.cause as Error | undefined)?.name : (error as Error | undefined)?.name
  return name === 'NotAllowedError' || name === 'AbortError'
}

/** The browser cannot do passkeys at all. */
function unsupported(error: unknown): boolean {
  return error instanceof Error && /not supported|does not support/i.test(error.message)
}
