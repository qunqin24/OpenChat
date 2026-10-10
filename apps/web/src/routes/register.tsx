import { createFileRoute, redirect } from '@tanstack/react-router'

import RegisterPage from '@/components/auth/RegisterPage'
import { sessionQueryOptions } from '@/features/auth/use-auth'
import { authOptionsQueryOptions } from '@/hooks/use-auth-options'

export const Route = createFileRoute('/register')({
  beforeLoad: async ({ context }) => {
    // The options are for the page to read; one that cannot be had is not worth
    // holding the page back for (it then offers registration, and the server decides).
    const [session] = await Promise.all([
      context.queryClient.ensureQueryData(sessionQueryOptions),
      context.queryClient.ensureQueryData(authOptionsQueryOptions).catch(() => undefined),
    ])
    if (session) throw redirect({ to: '/' })
  },
  component: RegisterPage,
})
