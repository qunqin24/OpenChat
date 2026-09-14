import { Trash2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Badge } from '@/components/shadcn/ui/badge'
import { Button } from '@/components/shadcn/ui/button'
import { Switch } from '@/components/shadcn/ui/switch'
import {
  useDeleteChannelModel,
  useUpdateChannelModel,
  type ChannelModel,
} from '../use-channel-models'

type ChannelModelRowProps = {
  channelId: string
  model: ChannelModel
  /** Set while a bulk run is going through the list this row is part of. */
  disabled?: boolean
}

/**
 * One model the channel serves.
 *
 * The row treats "the catalogue has nothing on this" as ordinary, because it
 * is: the endpoint is the authority on which models exist and models.dev only
 * decorates them, so most of a relay's ids will never be in it. The note sits
 * in the same slot, in the same muted type, as the provider name a catalogued
 * model gets — a different fact in that slot, not a fault.
 */
export function ChannelModelRow({ channelId, model, disabled }: ChannelModelRowProps) {
  const { t } = useTranslation()
  const updateModel = useUpdateChannelModel(channelId)
  const deleteModel = useDeleteChannelModel(channelId)

  const isManual = model.source === 'manual'
  const busy = disabled || updateModel.isPending || deleteModel.isPending

  const toggle = async (enabled: boolean) => {
    try {
      await updateModel.mutateAsync({ modelId: model.modelId, enabled })
    } catch {
      toast.error(t('admin.users.update-failed'))
    }
  }

  const remove = async () => {
    try {
      await deleteModel.mutateAsync(model.modelId)
    } catch {
      toast.error(t('admin.users.update-failed'))
    }
  }

  return (
    <div className="flex items-center gap-3 px-3 py-2">
      <div className="min-w-0 flex-1">
        <div className="flex items-center gap-1.5">
          <span className="truncate text-sm font-medium">{model.displayName}</span>
          {/* Only manual rows are badged. A fetched row is the ordinary case and
              badging both would be a column of noise. */}
          {isManual && (
            <Badge variant="outline" className="shrink-0 font-normal">
              {t('admin.channels.models-manual')}
            </Badge>
          )}
        </div>
        <div className="text-muted-foreground flex items-center gap-1.5 text-xs">
          {/* The id is what gets sent back to the endpoint, so it is shown
              verbatim and in a face that does not prettify it. */}
          <span className="truncate font-mono">{model.modelId}</span>
          <span aria-hidden="true">·</span>
          <span className="truncate">
            {model.catalog ? model.catalog.providerName : t('admin.channels.models-unknown')}
          </span>
        </div>
      </div>

      <Switch
        checked={model.enabled}
        disabled={busy}
        onCheckedChange={(value) => void toggle(value)}
        aria-label={model.displayName}
      />

      {/* Deleting a fetched row would be undone by the next refresh, so the
          action exists only for rows somebody typed. The empty slot keeps every
          switch in the list on one vertical line. */}
      {isManual ? (
        <Button
          type="button"
          variant="ghost"
          size="icon"
          className="text-muted-foreground hover:text-destructive size-8 shrink-0"
          disabled={busy}
          aria-label={t('admin.channels.models-remove')}
          title={t('admin.channels.models-remove')}
          onClick={() => void remove()}
        >
          <Trash2 className="size-4" />
        </Button>
      ) : (
        <span className="size-8 shrink-0" aria-hidden="true" />
      )}
    </div>
  )
}
