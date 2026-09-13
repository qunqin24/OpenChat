import type { Channel } from '@openchat/api-client'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Switch } from '@/components/shadcn/ui/switch'
import { useUpdateChannel } from '@/features/admin/use-admin'

type ChannelEnabledSwitchProps = {
  channel: Channel
}

/**
 * Turning a channel off is the one edit worth making without opening the
 * dialog, so it lives in the row. It PATCHes `enabled` alone — `apiKey` is not
 * in the body at all, which is what keeps the stored credential intact.
 *
 * A component rather than an inline cell so it can hold the mutation hook.
 */
export function ChannelEnabledSwitch({ channel }: ChannelEnabledSwitchProps) {
  const { t } = useTranslation()
  const updateChannel = useUpdateChannel()

  const toggle = async (enabled: boolean) => {
    try {
      await updateChannel.mutateAsync({ channelId: channel.id, enabled })
      toast.success(t('admin.channels.updated'))
    } catch {
      toast.error(t('admin.users.update-failed'))
    }
  }

  return (
    <Switch
      checked={channel.enabled}
      disabled={updateChannel.isPending}
      onCheckedChange={(value) => void toggle(value)}
      aria-label={t('admin.channels.enabled')}
    />
  )
}
