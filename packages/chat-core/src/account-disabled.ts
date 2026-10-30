import { isOpenChatError, type components } from '@openchat/api-client'

/**
 * A disabled account is not signed out (the server keeps its session): every
 * request it makes is answered 403 `account_disabled` with why - who disabled
 * it and when, the administrator's note, or what set a moderation ban off -
 * and whom to ask. Whichever call hears it first takes the page to /disabled,
 * which says so; signing in to such an account says the same.
 */
export type AccountDisabled = components['schemas']['AccountDisabled']

/** The details of a 403 account_disabled, or null for any other failure. */
export function accountDisabledOf(error: unknown): AccountDisabled | null {
  if (!isOpenChatError(error) || error.status !== 403 || error.code !== 'account_disabled') return null
  return disabledDetails(error.details)
}

/** An error's details read as AccountDisabled, whatever of them is missing. */
export function disabledDetails(value: unknown): AccountDisabled {
  const details = (value ?? {}) as Record<string, unknown>
  const admin = details.admin as Record<string, unknown> | undefined
  return {
    reason: details.reason === 'moderation' ? 'moderation' : 'admin',
    disabledAt: typeof details.disabledAt === 'string' ? details.disabledAt : '',
    ...(typeof details.note === 'string' && details.note ? { note: details.note } : {}),
    ...(typeof details.strikes === 'number' ? { strikes: details.strikes } : {}),
    ...(typeof details.windowHours === 'number' ? { windowHours: details.windowHours } : {}),
    ...(Array.isArray(details.categories)
      ? { categories: details.categories.filter((c): c is string => typeof c === 'string') }
      : {}),
    ...(admin && typeof admin.name === 'string' && typeof admin.email === 'string'
      ? { admin: { name: admin.name, email: admin.email } }
      : {}),
  }
}

/** A moderation category as its label's key under `admin.moderation.category`. */
export const categoryKey = (category: string) => category.replace('/', '-')
