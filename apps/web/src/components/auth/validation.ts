import type { AuthErrorKey } from '@/features/auth'

/** Minimum password length accepted at registration. */
export const MIN_PASSWORD_LENGTH = 8

// Deliberately loose: anything with a local part, an @, and a dotted domain.
// Strict RFC-shaped patterns reject valid addresses, and the server verifies
// the address for real anyway.
const EMAIL = /^[^\s@]+@[^\s@]+\.[^\s@]+$/

export function validateEmail(value: string): AuthErrorKey | null {
  if (!value.trim()) return 'auth.error.email-required'
  if (!EMAIL.test(value.trim())) return 'auth.error.email-invalid'
  return null
}

/**
 * Sign-in accepts an address or a username, so the only thing worth checking
 * here is that something was typed. Guessing which kind it is and applying the
 * email pattern would reject a perfectly good username that happens to contain
 * an @ — and the server has to check the value against the database regardless.
 */
export function validateIdentifier(value: string): AuthErrorKey | null {
  if (!value.trim()) return 'auth.error.identifier-required'
  return null
}

export function validateName(value: string): AuthErrorKey | null {
  if (!value.trim()) return 'auth.error.name-required'
  return null
}

export function validatePassword(
  value: string,
  { signup } = { signup: false },
): AuthErrorKey | null {
  if (!value) return 'auth.error.password-required'
  if (signup && value.length < MIN_PASSWORD_LENGTH) return 'auth.error.password-short'
  return null
}
