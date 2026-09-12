import { createFileRoute, redirect } from '@tanstack/react-router'

import LoginPage from '@/components/auth/LoginPage'
import { sessionQueryOptions } from '@/features/auth/use-auth'

export const Route = createFileRoute('/login')({
  validateSearch: (search: Record<string, unknown>): { redirect?: string } => ({
    redirect: typeof search.redirect === 'string' ? search.redirect : undefined,
  }),
  beforeLoad: async ({ context, search }) => {
    // Already signed in: skip the form and go where they were headed.
    const session = await context.queryClient.ensureQueryData(sessionQueryOptions)
    if (session) throw redirect({ to: search.redirect ?? '/' })
  },
  component: LoginPage,
})
