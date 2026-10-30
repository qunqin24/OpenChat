import { startAuthentication, WebAuthnError } from '@simplewebauthn/browser'

import { api, unwrap } from '@/lib/api'
import { adminOf, AuthFieldError, toAuthError, type ServerError, type SessionUser } from '@openchat/chat-core/auth'
import type { AuthApi } from './types'

/**
 * Auth against HttpOnly session cookies. The client sends `credentials:
 * 'include'` on every request (see lib/api.ts) — without it the browser
 * silently drops the cookie and every call looks signed out.
 */

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
