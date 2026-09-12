import { ShieldCheck, User as UserIcon, UserCheck, UserX } from 'lucide-react'

/** Faceted-filter options for the role column. */
export const roles = [
  { label: 'admin', value: 'admin', icon: ShieldCheck },
  { label: 'user', value: 'user', icon: UserIcon },
] as const

/** Faceted-filter options for the enabled/disabled column. */
export const statuses = [
  { label: 'enabled', value: 'enabled', icon: UserCheck },
  { label: 'disabled', value: 'disabled', icon: UserX },
] as const
