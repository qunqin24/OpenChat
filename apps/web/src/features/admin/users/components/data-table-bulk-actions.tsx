import type { User } from '@openchat/api-client'
import { type Table } from '@tanstack/react-table'
import { Shield, ShieldOff, UserCheck, UserRoundCheck, UserX } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { DataTableBulkActions as BulkActionsToolbar } from '@/components/shadcn/data-table'
import { Button } from '@/components/shadcn/ui/button'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/shadcn/ui/tooltip'
import { useBulkUpdateUsers } from '@/features/admin/use-admin'

type UsersBulkActionsProps<TData> = {
  table: Table<TData>
}

/**
 * Upstream fakes these with a sleep and a toast; here they hit the bulk
 * endpoint, which applies the change in one transaction and reports back which
 * accounts it refused to touch (your own, or one that no longer exists).
 *
 * "Approve" lets in the selected accounts that are waiting (the others are left
 * as they are, and the count says how many were). Deleting accounts is deliberately absent: there is no such endpoint, and
 * removing a user would cascade away every conversation they own. Disabling is
 * the reversible equivalent.
 */
export function UsersBulkActions<TData>({ table }: UsersBulkActionsProps<TData>) {
  const { t } = useTranslation()
  const bulkUpdate = useBulkUpdateUsers()

  const selected = table.getFilteredSelectedRowModel().rows.map((row) => row.original as User)

  const apply = async (body: { role?: User['role']; disabled?: boolean; approved?: boolean }) => {
    if (selected.length === 0) return

    try {
      const result = await bulkUpdate.mutateAsync({
        userIds: selected.map((user) => user.id),
        ...body,
      })

      table.resetRowSelection()

      const parts = [t('admin.bulk.done', { n: result.updated })]
      if (result.skipped.length > 0) parts.push(t('admin.bulk.skipped', { n: result.skipped.length }))
      toast.success(parts.join(' · '))
    } catch {
      // As the row actions do: say so, and leave the selection for another try.
      toast.error(t('admin.bulk.failed'))
    }
  }

  const actions = [
    { key: 'approve', icon: UserRoundCheck, body: { approved: true }, variant: 'secondary' as const },
    { key: 'promote', icon: Shield, body: { role: 'admin' as const }, variant: 'secondary' as const },
    { key: 'demote', icon: ShieldOff, body: { role: 'user' as const }, variant: 'secondary' as const },
    { key: 'enable', icon: UserCheck, body: { disabled: false }, variant: 'secondary' as const },
    { key: 'disable', icon: UserX, body: { disabled: true }, variant: 'destructive' as const },
  ]

  return (
    <BulkActionsToolbar table={table} entityName={t('admin.bulk.entity-user')}>
      {actions.map(({ key, icon: Icon, body, variant }) => (
        <Tooltip key={key}>
          <TooltipTrigger asChild>
            <Button
              variant={variant}
              size="icon"
              className="size-8"
              disabled={bulkUpdate.isPending}
              aria-label={t(`admin.bulk.${key}` as 'admin.bulk.promote')}
              onClick={() => void apply(body)}
            >
              <Icon />
            </Button>
          </TooltipTrigger>
          <TooltipContent>
            <p>{t(`admin.bulk.${key}` as 'admin.bulk.promote')}</p>
          </TooltipContent>
        </Tooltip>
      ))}
    </BulkActionsToolbar>
  )
}
