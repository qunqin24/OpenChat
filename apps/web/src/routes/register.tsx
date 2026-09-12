import { createFileRoute, redirect } from '@tanstack/react-router'

import RegisterPage from '@/components/auth/RegisterPage'
import { sessionQueryOptions } from '@/features/auth/use-auth'

export const Route = createFileRoute('/register')({
  beforeLoad: async ({ context }) => {
    const session = await context.queryClient.ensureQueryData(sessionQueryOptions)
    if (session) throw redirect({ to: '/' })
  },
  component: RegisterPage,
})
