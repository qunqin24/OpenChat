import { isOpenChatError, type User } from '@openchat/api-client'
import { MoreHorizontal, Trash2, UserCheck, UserPen, UserRoundCheck, UserX } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { ConfirmDialog } from '@/components/shadcn/confirm-dialog'
import { Button } from '@/components/shadcn/ui/button'
import { Input } from '@/components/shadcn/ui/input'
import { Label } from '@/components/shadcn/ui/label'
import { Textarea } from '@/components/shadcn/ui/textarea'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuShortcut,
  DropdownMenuTrigger,
} from '@/components/shadcn/ui/dropdown-menu'
import { useDeleteUser, useUpdateUser } from '@/features/admin/use-admin'
import { UsersActionDialog } from './users-action-dialog'

type UsersRowActionsProps = {
  user: User
  /** The signed-in admin cannot demote or disable themselves. */
  isSelf: boolean
}

/**
 * Disabling cuts off access now and is reversible from the same menu; deleting
 * removes the account with every conversation, file and memory it has, for
 * good, so it asks for the account's name to be typed, as upstream's delete
 * dialog does. Neither is offered for the signed-in admin's own account.
 *
 * An account that signed itself up and is waiting for a decision also gets
 * "approve", and its disable item reads "reject": rejecting is disabling, so a
 * rejected sign-up is an ordinary disabled account that can be enabled again
 * (and then is back among those waiting).
 */
export function UsersRowActions({ user, isSelf }: UsersRowActionsProps) {
  const { t } = useTranslation()
  const updateUser = useUpdateUser()
  const deleteUser = useDeleteUser()
  const [editing, setEditing] = useState(false)
  const [confirming, setConfirming] = useState(false)
  const [deleting, setDeleting] = useState(false)
  const [typed, setTyped] = useState('')
  // What the person is told when disabled: optional, shown on their page.
  const [note, setNote] = useState('')

  // Waiting for a decision: not yet disabled (a rejected sign-up is disabled,
  // and from then on is an ordinary disabled account, which can be enabled).
  const awaiting = user.pending && !user.disabled

  const setDisabled = async (disabled: boolean) => {
    try {
      await updateUser.mutateAsync({
        userId: user.id,
        disabled,
        ...(disabled && note.trim() ? { disabledNote: note.trim() } : {}),
      })
      toast.success(
        t(disabled ? (awaiting ? 'admin.users.reject-done' : 'admin.users.disabled-done') : 'admin.users.enabled-done'),
      )
      setConfirming(false)
    } catch {
      toast.error(t('admin.users.update-failed'))
    }
  }

  const remove = async () => {
    try {
      await deleteUser.mutateAsync(user.id)
      toast.success(t('admin.users.delete-done', { name: user.name }))
      setDeleting(false)
    } catch (error) {
      toast.error(isOpenChatError(error) && error.code === 'last_admin' ? t('admin.users.last-admin') : t('admin.users.delete-failed'))
    }
  }

  const approve = async () => {
    try {
      await updateUser.mutateAsync({ userId: user.id, approved: true })
      toast.success(t('admin.users.approve-done'))
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

          {user.pending && (
            <DropdownMenuItem onClick={() => void approve()}>
              {t('admin.users.approve')}
              <DropdownMenuShortcut>
                <UserRoundCheck size={16} />
              </DropdownMenuShortcut>
            </DropdownMenuItem>
          )}

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
              onClick={() => {
                setNote('')
                setConfirming(true)
              }}
            >
              {t(awaiting ? 'admin.users.reject' : 'admin.users.disable')}
              <DropdownMenuShortcut>
                <UserX size={16} />
              </DropdownMenuShortcut>
            </DropdownMenuItem>
          )}

          <DropdownMenuItem
            variant="destructive"
            disabled={isSelf}
            onClick={() => {
              setTyped('')
              setDeleting(true)
            }}
          >
            {t('admin.users.delete')}
            <DropdownMenuShortcut>
              <Trash2 size={16} />
            </DropdownMenuShortcut>
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>

      {/* Not wrapped in `open && …`: Radix unmounts the content itself once
          the exit animation has finished, and removing the element early is
          what leaves an overlay that animates in and then simply vanishes. The
          form still starts fresh every time, because its fields only mount
          when the overlay opens. */}
      <UsersActionDialog user={user} isSelf={isSelf} open={editing} onOpenChange={setEditing} />

      <ConfirmDialog
        open={confirming}
        onOpenChange={setConfirming}
        title={t(awaiting ? 'admin.users.reject' : 'admin.users.disable')}
        desc={t(awaiting ? 'admin.users.reject-confirm' : 'admin.users.disable-confirm', { name: user.name })}
        cancelBtnText={t('admin.ui.cancel')}
        confirmText={t(awaiting ? 'admin.users.reject' : 'admin.users.disable')}
        destructive
        isLoading={updateUser.isPending}
        handleConfirm={() => void setDisabled(true)}
        className="sm:max-w-md"
      >
        <div className="grid gap-2">
          <Label htmlFor={`disable-note-${user.id}`}>{t('admin.users.disable-note')}</Label>
          <Textarea
            id={`disable-note-${user.id}`}
            value={note}
            maxLength={500}
            rows={3}
            placeholder={t('admin.users.disable-note-placeholder')}
            onChange={(event) => setNote(event.target.value)}
          />
          <p className="text-muted-foreground text-xs">{t('admin.users.disable-note-hint')}</p>
        </div>
      </ConfirmDialog>

      <ConfirmDialog
        open={deleting}
        onOpenChange={setDeleting}
        title={t('admin.users.delete-title')}
        desc={t('admin.users.delete-confirm', { name: user.name })}
        cancelBtnText={t('admin.ui.cancel')}
        confirmText={t('admin.users.delete')}
        destructive
        disabled={typed.trim() !== user.name}
        isLoading={deleteUser.isPending}
        handleConfirm={() => void remove()}
        className="sm:max-w-md"
      >
        <div className="grid gap-2">
          <Label htmlFor={`delete-${user.id}`}>{t('admin.users.delete-type', { name: user.name })}</Label>
          <Input
            id={`delete-${user.id}`}
            value={typed}
            autoComplete="off"
            placeholder={user.name}
            onChange={(event) => setTyped(event.target.value)}
          />
        </div>
      </ConfirmDialog>
    </>
  )
}
