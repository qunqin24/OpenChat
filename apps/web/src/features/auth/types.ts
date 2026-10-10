import type { SessionUser as GeneratedSessionUser } from '@openchat/api-client'

/** The signed-in user, straight from the contract. */
export type SessionUser = GeneratedSessionUser

export interface LoginInput {
  /** An email address or a username; the server works out which. */
  identifier: string
  password: string
}

export interface RegisterInput {
  email: string
  name: string
  password: string
}

/**
 * What registering came to. The account either works at once (and the server
 * has signed it in), or is waiting for an administrator and has no session.
 */
export type RegisterOutcome = { status: 'signed_in'; user: SessionUser } | { status: 'pending' }

/** Only the fields present are changed. */
export interface UpdateProfileInput {
  name?: string
  email?: string
}

export interface ChangePasswordInput {
  currentPassword: string
  newPassword: string
}

/**
 * Every auth call goes through this interface.
 *
 * Shaped for HttpOnly session cookies: nothing here returns or accepts a token,
 * because the browser carries the cookie and JavaScript never sees it. "Am I
 * signed in?" is answered by asking the server, not by reading local state.
 */
export interface AuthApi {
  /** Resolves to null when there is no valid session. */
  session(): Promise<SessionUser | null>
  login(input: LoginInput): Promise<SessionUser>
  register(input: RegisterInput): Promise<RegisterOutcome>
  logout(): Promise<void>
  /** Self-service: cannot reach role or enabled state, by contract. */
  updateProfile(input: UpdateProfileInput): Promise<SessionUser>
  /** Requires the current password; other sessions are revoked, this one is not. */
  changePassword(input: ChangePasswordInput): Promise<void>
}

/**
 * Every auth failure the UI can show, as an i18n key. Keeping it a union rather
 * than a free string means `t(error.key)` type-checks against the locale files,
 * so a renamed message breaks the build instead of rendering the raw key.
 */
export type AuthErrorKey =
  | 'auth.error.invalid-credentials'
  | 'auth.error.email-taken'
  | 'auth.error.name-taken'
  | 'auth.error.email-required'
  | 'auth.error.email-invalid'
  | 'auth.error.identifier-required'
  | 'auth.error.name-required'
  | 'auth.error.name-invalid'
  | 'auth.error.password-required'
  | 'auth.error.password-short'
  | 'auth.error.account-disabled'
  | 'auth.error.pending-approval'
  | 'auth.error.registration-closed'
  | 'auth.error.login-rate-limited'
  | 'auth.error.register-rate-limited'
  | 'auth.error.password-wrong'
  | 'auth.error.nothing-to-update'
  | 'auth.error.unknown'

/** Field-level failures the forms surface next to the offending input. */
export class AuthFieldError extends Error {
  readonly field:
    | 'form'
    | 'identifier'
    | 'email'
    | 'name'
    | 'password'
    | 'currentPassword'
    | 'newPassword'
  readonly key: AuthErrorKey

  constructor(field: AuthFieldError['field'], key: AuthErrorKey) {
    super(key)
    this.name = 'AuthFieldError'
    this.field = field
    this.key = key
  }
}

export function isAuthFieldError(error: unknown): error is AuthFieldError {
  return error instanceof AuthFieldError
}
