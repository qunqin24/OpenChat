import { useQueryClient } from '@tanstack/react-query'
import { useNavigate } from '@tanstack/react-router'
import { useState } from 'react'

import { sessionQueryOptions, useSignOut } from '@/features/auth/use-auth'
import { accountDisabledOf } from '@openchat/chat-core/account-disabled'
import { useAccountDisabled } from '@/lib/account-disabled'
import { api, unwrap } from '@/lib/api'
import { DisabledScreen } from './AccountOverlay'

/**
 * /disabled: the app's place for a signed-in account that has been disabled
 * (lib/account-disabled.ts). It shows what the server last said (the route
 * asks again when opened afresh). Asking again is also how it lets the person
 * back in: once the account is enabled the session answers, and the page goes
 * home.
 */
export default function DisabledPage() {
  const details = useAccountDisabled((s) => s.details)
  const setDetails = useAccountDisabled((s) => s.set)
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const signOut = useSignOut()
  const [checking, setChecking] = useState(false)
  const [still, setStill] = useState(false)

  const check = async () => {
    setChecking(true)
    try {
      const user = unwrap(await api.GET('/auth/session'))
      setDetails(null)
      queryClient.setQueryData(sessionQueryOptions.queryKey, user)
      // What was fetched while it was disabled failed; it is asked again.
      void queryClient.invalidateQueries()
      await navigate({ to: '/', replace: true })
    } catch (error) {
      const disabled = accountDisabledOf(error)
      if (disabled) {
        setDetails(disabled)
        setStill(true)
      } else {
        await navigate({ to: '/login', replace: true })
      }
    } finally {
      setChecking(false)
    }
  }

  const leave = () => {
    setDetails(null)
    void signOut()
  }

  if (!details) return null
  return (
    <DisabledScreen
      details={details}
      checking={checking}
      still={still}
      onCheck={() => void check()}
      onSignOut={leave}
    />
  )
}
