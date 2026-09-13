import type { User } from '@openchat/api-client'
import { MoreHorizontal, UserCheck, UserPen, UserX } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { ConfirmDialog } from '@/components/shadcn/confirm-dialog'
import { Button } from '@/components/shadcn/ui/button'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuShortcut,
  DropdownMenuTrigger,
} from '@/components/shadcn/ui/dropdown-menu'
import { useUpdateUser } from '@/features/admin/use-admin'
import { UsersActionDialog } from './users-action-dialog'

type UsersRowActionsProps = {
  user: User
  /** The signed-in admin cannot demote or disable themselves. */
  isSelf: boolean
}

/**
 * Upstream's destructive item deletes the account. There is no such endpoint
 * here on purpose — removing a user would cascade away every conversation they
 * own — so the second item disables instead, which cuts off access now and is
 * reversible from the same menu.
 */
export function UsersRowActions({ user, isSelf }: UsersRowActionsProps) {
  const { t } = useTranslation()
  const updateUser = useUpdateUser()
  const [editing, setEditing] = useState(false)
  const [confirming, setConfirming] = useState(false)

  const setDisabled = async (disabled: boolean) => {
    try {
      await updateUser.mutateAsync({ userId: user.id, disabled })
      toast.success(t(disabled ? 'admin.users.disabled-done' : 'admin.users.enabled-done'))
      setConfirming(false)
    } catch {
      toast.error(t('admin.users.update-failed'))
    }
  }

  return (
    <>
      <DropdownMenu modal={false}>
        <DropdownMenuTrigger asChild>
          <Button
            variant="ghost"
            className="data-[state=open]:bg-muted flex size-8 p-0"
            aria-label={t('admin.users.actions')}
          >
            <MoreHorizontal className="size-4" />
          </Button>
        </DropdownMenuTrigger>

        <DropdownMenuContent align="end" className="w-40">
          <DropdownMenuItem onClick={() => setEditing(true)}>
            {t('admin.users.edit')}
            <DropdownMenuShortcut>
              <UserPen size={16} />
            </DropdownMenuShortcut>
          </DropdownMenuItem>

          <DropdownMenuSeparator />

          {user.disabled ? (
            <DropdownMenuItem onClick={() => void setDisabled(false)}>
              {t('admin.users.enable')}
              <DropdownMenuShortcut>
                <UserCheck size={16} />
              </DropdownMenuShortcut>
            </DropdownMenuItem>
          ) : (
            <DropdownMenuItem
              variant="destructive"
              disabled={isSelf}
              onClick={() => setConfirming(true)}
            >
              {t('admin.users.disable')}
              <DropdownMenuShortcut>
                <UserX size={16} />
              </DropdownMenuShortcut>
            </DropdownMenuItem>
          )}
        </DropdownMenuContent>
      </DropdownMenu>

      {/* Mounted only while open, so the form starts from the row as it
          currently stands rather than from whatever it held last time. */}
      {editing && (
        <UsersActionDialog user={user} isSelf={isSelf} open onOpenChange={setEditing} />
      )}

      <ConfirmDialog
        open={confirming}
        onOpenChange={setConfirming}
        title={t('admin.users.disable')}
        desc={t('admin.users.disable-confirm', { name: user.name })}
        cancelBtnText={t('admin.ui.cancel')}
        confirmText={t('admin.users.disable')}
        destructive
        isLoading={updateUser.isPending}
        handleConfirm={() => void setDisabled(true)}
        className="sm:max-w-sm"
      />
    </>
  )
}
