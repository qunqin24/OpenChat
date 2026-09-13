import type { Channel } from '@openchat/api-client'
import { MoreHorizontal, SquarePen, Trash2 } from 'lucide-react'
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
import { useDeleteChannel } from '@/features/admin/use-admin'
import { ChannelsActionDialog } from './channels-action-dialog'

type ChannelsRowActionsProps = {
  channel: Channel
}

/**
 * The users table disables instead of deleting, because an account owns
 * conversations that would cascade away with it. A channel owns nothing —
 * removing one drops the connection and its key and leaves every message in
 * place — so here the destructive item really does delete.
 */
export function ChannelsRowActions({ channel }: ChannelsRowActionsProps) {
  const { t } = useTranslation()
  const deleteChannel = useDeleteChannel()
  const [editing, setEditing] = useState(false)
  const [confirming, setConfirming] = useState(false)

  const remove = async () => {
    try {
      await deleteChannel.mutateAsync(channel.id)
      toast.success(t('admin.channels.deleted'))
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
            {t('admin.channels.edit')}
            <DropdownMenuShortcut>
              <SquarePen size={16} />
            </DropdownMenuShortcut>
          </DropdownMenuItem>

          <DropdownMenuSeparator />

          <DropdownMenuItem variant="destructive" onClick={() => setConfirming(true)}>
            {t('admin.channels.delete')}
            <DropdownMenuShortcut>
              <Trash2 size={16} />
            </DropdownMenuShortcut>
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>

      {/* Mounted only while open, so the form starts from the row as it
          currently stands rather than from whatever it held last time. */}
      {editing && <ChannelsActionDialog channel={channel} open onOpenChange={setEditing} />}

      <ConfirmDialog
        open={confirming}
        onOpenChange={setConfirming}
        title={t('admin.channels.delete')}
        desc={t('admin.channels.delete-confirm', { name: channel.name })}
        cancelBtnText={t('admin.ui.cancel')}
        confirmText={t('admin.channels.delete')}
        destructive
        isLoading={deleteChannel.isPending}
        handleConfirm={() => void remove()}
        className="sm:max-w-sm"
      />
    </>
  )
}
