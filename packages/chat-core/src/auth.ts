import type { SessionUser as GeneratedSessionUser } from '@openchat/api-client'

import { disabledDetails, type AccountDisabled } from './account-disabled'

// Signing in, as every client does it: the inputs, the outcomes, and the
// server's error codes as the message keys a form shows. The calls
// themselves are each app's (a browser's cookie, an app's token).

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
/** Who an account that cannot sign in is told to ask; see AdminContact in the contract. */
export interface AdminContact {
  name: string
  email: string
}

export type RegisterOutcome =
  | { status: 'signed_in'; user: SessionUser }
  | { status: 'pending'; admin?: AdminContact }

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
  | 'auth.error.invalid-passkey'
  | 'auth.error.passkey-unsupported'
  | 'auth.error.device-passkey-unsupported'
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
  /** On pending_approval and account_disabled: whom to ask, when the server named someone. */
  readonly admin?: AdminContact
  /** On account_disabled: why, as the server told it (lib/account-disabled.ts). */
  readonly disabled?: AccountDisabled

  constructor(field: AuthFieldError['field'], key: AuthErrorKey, admin?: AdminContact, disabled?: AccountDisabled) {
    super(key)
    this.name = 'AuthFieldError'
    this.field = field
    this.key = key
    this.admin = admin
    this.disabled = disabled
  }
}

export function isAuthFieldError(error: unknown): error is AuthFieldError {
  return error instanceof AuthFieldError
}

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

export interface ServerError {
  code?: string
  field?: AuthFieldError['field']
  details?: { admin?: unknown }
}

/** details.admin when it has the shape of an AdminContact; anything else is no contact. */
export function adminOf(value: unknown): AdminContact | undefined {
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

export function toAuthError(error: unknown): AuthFieldError {
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
