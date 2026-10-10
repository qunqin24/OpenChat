import { Clock, ShieldCheck, User as UserIcon, UserCheck, UserX } from 'lucide-react'

/** Faceted-filter options for the role column. */
export const roles = [
  { label: 'admin', value: 'admin', icon: ShieldCheck },
  { label: 'user', value: 'user', icon: UserIcon },
] as const

/**
 * Faceted-filter options for the status column. These are the server's
 * `UserStatus` values, sent as the list's `status` filter. A disabled account
 * is `disabled` even when it is also waiting (a rejected sign-up), so
 * "pending" is the list of those still waiting for a decision.
 */
export const statuses = [
  { label: 'pending', value: 'pending', icon: Clock },
  { label: 'enabled', value: 'enabled', icon: UserCheck },
  { label: 'disabled', value: 'disabled', icon: UserX },
] as const
