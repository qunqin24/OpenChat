import { createFileRoute, redirect } from '@tanstack/react-router'

import DisabledPage from '@/components/auth/DisabledPage'
import { sessionQueryOptions } from '@/features/auth/use-auth'
import { accountDisabledOf } from '@openchat/chat-core/account-disabled'
import { useAccountDisabled } from '@/lib/account-disabled'
import { api, unwrap } from '@/lib/api'

/**
 * A disabled account's page (lib/account-disabled.ts), outside the signed-in
 * layout, whose guard sends it here. Opened afresh - a reload - it asks the
 * session what to show: why it is disabled, or, enabled or signed out since,
 * the way on.
 */
export const Route = createFileRoute('/disabled')({
  beforeLoad: async ({ context }) => {
    if (useAccountDisabled.getState().details) return
    try {
      const user = unwrap(await api.GET('/auth/session'))
      context.queryClient.setQueryData(sessionQueryOptions.queryKey, user)
    } catch (error) {
      const disabled = accountDisabledOf(error)
      if (disabled) {
        useAccountDisabled.getState().set(disabled)
        return
      }
      throw redirect({ to: '/login', replace: true })
    }
    throw redirect({ to: '/', replace: true })
  },
  component: DisabledPage,
})
