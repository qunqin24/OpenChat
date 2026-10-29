import { isOpenChatError, type components } from '@openchat/api-client'
import { create } from 'zustand'

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

interface AccountDisabledState {
  /** What the server said last, for /disabled to show; null until it has. */
  details: AccountDisabled | null
  set: (details: AccountDisabled | null) => void
}

export const useAccountDisabled = create<AccountDisabledState>((set) => ({
  details: null,
  set: (details) => set({ details }),
}))

/** The part of the router this needs; the real one fits. */
export interface DisabledRouter {
  latestLocation: { pathname: string }
  navigate: (options: { to: '/disabled'; replace: boolean }) => Promise<unknown>
}

/**
 * Handles one failure from anywhere: if it says the account is disabled, keeps
 * why and takes the page to /disabled. The sign-in forms show their own.
 */
export async function showIfAccountDisabled(error: unknown, router: DisabledRouter): Promise<void> {
  const details = accountDisabledOf(error)
  if (!details) return
  useAccountDisabled.getState().set(details)
  const { pathname } = router.latestLocation
  if (pathname === '/disabled' || pathname === '/login' || pathname === '/register') return
  await router.navigate({ to: '/disabled', replace: true })
}

/** A moderation category as its label's key under `admin.moderation.category`. */
export const categoryKey = (category: string) => category.replace('/', '-')
