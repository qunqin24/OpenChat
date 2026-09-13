import { Plus } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/shadcn/ui/button'
import { ChannelsActionPanel } from './channels-action-panel'

export function ChannelsPrimaryButtons() {
  const { t } = useTranslation()
  const [creating, setCreating] = useState(false)

  return (
    <>
      <Button className="space-x-1" onClick={() => setCreating(true)}>
        <span>{t('admin.channels.create')}</span>
        <Plus size={18} />
      </Button>

      {/* Not wrapped in `open && …`: Radix unmounts the content itself once
          the exit animation has finished, and removing the element early is
          what leaves an overlay that animates in and then simply vanishes. The
          form still starts fresh every time, because its fields only mount
          when the overlay opens. */}
      <ChannelsActionPanel open={creating} onOpenChange={setCreating} />
    </>
  )
}
