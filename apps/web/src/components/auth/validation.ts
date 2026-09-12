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
