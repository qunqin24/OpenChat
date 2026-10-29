import { useTranslation } from 'react-i18next'

import { ConfirmDialog } from '@/components/shadcn/confirm-dialog'
import { useSignOut } from '@/features/auth/use-auth'

interface SignOutDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
}

/**
 * Upstream resets a client-side auth store; sessions here are HttpOnly cookies,
 * so signing out is a server call. No `redirect` is carried back to the login
 * page either — whoever signs out of the admin panel is not trying to return
 * to the page they were on.
 */
export function SignOutDialog({ open, onOpenChange }: SignOutDialogProps) {
  const { t } = useTranslation()
  const signOut = useSignOut()

  return (
    <ConfirmDialog
      open={open}
      onOpenChange={onOpenChange}
      title={t('auth.logout')}
      desc={t('admin.sign-out-confirm')}
      confirmText={t('auth.logout')}
      destructive
      handleConfirm={() => void signOut()}
      className="sm:max-w-sm"
    />
  )
}
