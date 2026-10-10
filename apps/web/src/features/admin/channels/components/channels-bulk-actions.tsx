import type { Channel } from '@openchat/api-client'
import { type Table } from '@tanstack/react-table'
import { Power, PowerOff, Trash2 } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { ConfirmDialog } from '@/components/shadcn/confirm-dialog'
import { DataTableBulkActions as BulkActionsToolbar } from '@/components/shadcn/data-table'
import { Button } from '@/components/shadcn/ui/button'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/shadcn/ui/tooltip'
import { useBulkChannels } from '@/features/admin/use-admin'

/**
 * The floating toolbar for selected channels, as on the users table: enable,
 * disable, and — since a channel owns no conversations — delete, behind a
 * confirmation that says how many.
 */
export function ChannelsBulkActions({ table }: { table: Table<Channel> }) {
  const { t } = useTranslation()
  const bulk = useBulkChannels()
  const [confirming, setConfirming] = useState(false)

  const selected = table.getFilteredSelectedRowModel().rows.map((row) => row.original)

  const apply = async (action: 'enable' | 'disable' | 'delete') => {
    if (selected.length === 0) return
    const result = await bulk.mutateAsync({ channelIds: selected.map((channel) => channel.id), action })
    table.resetRowSelection()
    setConfirming(false)
    const parts = [
      action === 'delete'
        ? t('admin.bulk.channels-deleted', { n: result.done })
        : t('admin.bulk.channels-updated', { n: result.done }),
    ]
    if (result.failed > 0) {
      parts.push(t('admin.bulk.failed', { n: result.failed }))
      toast.error(parts.join(' · '))
    } else {
      toast.success(parts.join(' · '))
    }
  }

  const actions = [
    { key: 'enable', icon: Power, variant: 'secondary' as const, run: () => void apply('enable') },
    { key: 'disable', icon: PowerOff, variant: 'secondary' as const, run: () => void apply('disable') },
    { key: 'delete', icon: Trash2, variant: 'destructive' as const, run: () => setConfirming(true) },
  ] as const

  return (
    <>
      <BulkActionsToolbar table={table} entityName={t('admin.bulk.entity-channel')}>
        {actions.map(({ key, icon: Icon, variant, run }) => (
          <Tooltip key={key}>
            <TooltipTrigger asChild>
              <Button
                variant={variant}
                size="icon"
                className="size-8"
                disabled={bulk.isPending}
                aria-label={t(`admin.bulk.${key}`)}
                onClick={run}
              >
                <Icon />
              </Button>
            </TooltipTrigger>
            <TooltipContent>
              <p>{t(`admin.bulk.${key}`)}</p>
            </TooltipContent>
          </Tooltip>
        ))}
      </BulkActionsToolbar>

      <ConfirmDialog
        open={confirming}
        onOpenChange={setConfirming}
        title={t('admin.bulk.channels-delete-title', { n: selected.length })}
        desc={t('admin.bulk.channels-delete-confirm', { n: selected.length })}
        cancelBtnText={t('admin.ui.cancel')}
        confirmText={t('admin.bulk.delete')}
        destructive
        isLoading={bulk.isPending}
        handleConfirm={() => void apply('delete')}
        className="sm:max-w-sm"
      />
    </>
  )
}
