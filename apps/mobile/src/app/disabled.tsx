import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { AccountNotice, DisabledReason, useDisabledWords } from '@/components/AccountNotice'
import { signOut, useSession } from '@/lib/session'

/**
 * A signed-in account that has been disabled: the session is kept, so this
 * says why in place of the app. Checking again asks the server who the token
 * is; once the account is enabled the session says so and the guard goes on.
 */
export default function DisabledScreen() {
  const { t } = useTranslation()
  const session = useSession()
  const [checked, setChecked] = useState(false)
  const [leaving, setLeaving] = useState(false)
  const details = session.data?.state === 'disabled' ? session.data.details : undefined
  const words = useDisabledWords(details)
  if (!details) return null

  return (
    <AccountNotice
      tone="refused"
      {...words}
      admin={details.admin}
      status={checked && !session.isFetching ? t('auth.disabled.still') : undefined}
      action={{
        label: t('auth.disabled.sign-out'),
        busy: leaving,
        onPress: () => {
          setLeaving(true)
          void signOut().finally(() => setLeaving(false))
        },
      }}
      secondary={{
        label: session.isFetching ? t('auth.disabled.checking') : t('auth.disabled.check'),
        onPress: () => {
          setChecked(true)
          void session.refetch()
        },
      }}
    >
      <DisabledReason details={details} />
    </AccountNotice>
  )
}
