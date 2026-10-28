import { createFileRoute, redirect } from '@tanstack/react-router'

import LoginPage from '@/components/auth/LoginPage'
import { sessionQueryOptions } from '@/features/auth/use-auth'
import { authOptionsQueryOptions } from '@/hooks/use-auth-options'

/** Login resumes a chat, never an admin page saved in a previous return URL. */
function chatReturnPath(value: unknown): string | undefined {
  if (typeof value !== 'string' || !value.startsWith('/')) return undefined

  try {
    const base = 'https://openchat.invalid'
    const target = new URL(value, base)
    if (target.origin !== base) return undefined
    // Normalize first so /chat/../admin cannot bypass the allowed paths.
    if (target.pathname !== '/' && !/^\/chat\/[^/]+$/.test(target.pathname)) return undefined
    return `${target.pathname}${target.search}${target.hash}`
  } catch {
    return undefined
  }
}

export const Route = createFileRoute('/login')({
  validateSearch: (search: Record<string, unknown>): { redirect?: string } => ({
    redirect: chatReturnPath(search.redirect),
  }),
  beforeLoad: async ({ context, search }) => {
    // Use the same chat-only destination as the successful form submission.
    const [session] = await Promise.all([
      context.queryClient.ensureQueryData(sessionQueryOptions),
      // For the page to read (the registration link); not
      // worth holding it back for if it cannot be had.
      context.queryClient.ensureQueryData(authOptionsQueryOptions).catch(() => undefined),
    ])
    if (session) throw redirect({ to: search.redirect ?? '/' })
  },
  component: LoginPage,
})
