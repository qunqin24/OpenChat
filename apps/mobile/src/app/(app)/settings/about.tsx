import * as Application from 'expo-application'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Group, Row, SettingsPage } from '@/components/settings/parts'
import { serverUrl } from '@/config'
import { checkForUpdates, updatesSupported } from '@/lib/update'
import { toast } from '@/lib/toast'

/**
 * The app itself: its version, the server it talks to, and a way to ask for
 * an update now rather than at the next automatic check. A new version opens
 * the update prompt on its own (UpdatePrompt, mounted at the root).
 */
export default function AboutScreen() {
  const { t } = useTranslation()
  const [checking, setChecking] = useState(false)

  const check = async () => {
    setChecking(true)
    try {
      const result = await checkForUpdates({ manual: true })
      if (result.status === 'latest') toast.info(t('mobile.update.latest'))
      else if (result.status === 'failed') toast.error(t('mobile.update.failed'))
    } finally {
      setChecking(false)
    }
  }

  return (
    <SettingsPage title={t('mobile.settings.about')}>
      <Group>
        <Row label={t('mobile.settings.version')} value={Application.nativeApplicationVersion ?? '-'} />
        <Row label={t('mobile.settings.server')} value={serverUrl.replace(/^https?:\/\//, '')} />
      </Group>
      {updatesSupported ? (
        <Group>
          <Row
            label={checking ? t('mobile.update.checking') : t('mobile.update.check')}
            disabled={checking}
            chevron={false}
            onPress={() => void check()}
          />
        </Group>
      ) : null}
    </SettingsPage>
  )
}
