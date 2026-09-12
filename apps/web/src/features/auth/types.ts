/** The signed-in user, as the session endpoint reports them. */
export interface SessionUser {
  id: string
  email: string
  name: string
}

export interface LoginInput {
  email: string
  password: string
}

export interface RegisterInput {
  email: string
  name: string
  password: string
}

/**
 * Every auth call goes through this interface, so swapping the mock for the
 * real backend is a one-line change in `features/auth/index.ts`.
 *
 * Shaped for HttpOnly session cookies: nothing here returns or accepts a token,
 * because the browser carries the cookie and JavaScript never sees it. "Am I
 * signed in?" is answered by asking the server, not by reading local state.
 */
export interface AuthApi {
  /** Resolves to null when there is no valid session. */
  session(): Promise<SessionUser | null>
  login(input: LoginInput): Promise<SessionUser>
  register(input: RegisterInput): Promise<SessionUser>
  logout(): Promise<void>
}

/**
 * Every auth failure the UI can show, as an i18n key. Keeping it a union rather
 * than a free string means `t(error.key)` type-checks against the locale files,
 * so a renamed message breaks the build instead of rendering the raw key.
 */
export type AuthErrorKey =
  | 'auth.error.invalid-credentials'
  | 'auth.error.email-taken'
  | 'auth.error.email-required'
  | 'auth.error.email-invalid'
  | 'auth.error.name-required'
  | 'auth.error.password-required'
  | 'auth.error.password-short'
  | 'auth.error.unknown'

/** Field-level failures the forms surface next to the offending input. */
export class AuthFieldError extends Error {
  readonly field: 'email' | 'name' | 'password'
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
