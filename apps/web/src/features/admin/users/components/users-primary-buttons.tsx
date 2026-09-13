import { UserPlus } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/shadcn/ui/button'
import { UsersActionDialog } from './users-action-dialog'

/**
 * Upstream pairs this with an "Invite User" button that mails an invitation.
 * There is no mail transport here, so the admin sets the password and passes
 * it on out of band.
 */
export function UsersPrimaryButtons() {
  const { t } = useTranslation()
  const [creating, setCreating] = useState(false)

  return (
    <>
      <Button className="space-x-1" onClick={() => setCreating(true)}>
        <span>{t('admin.users.create')}</span>
        <UserPlus size={18} />
      </Button>

      {creating && <UsersActionDialog open onOpenChange={setCreating} />}
    </>
  )
}
