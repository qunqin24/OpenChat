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

      {/* Mounted only while open, so every new channel starts from a blank
          form — including the key field, which has nothing to restore. */}
      {creating && <ChannelsActionPanel open onOpenChange={setCreating} />}
    </>
  )
}
