import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { captureScreen } from '@/lib/attach-sources'

/**
 * Taking a screenshot to attach, with what can go wrong said in a toast: from
 * the menu's item, and from the same item typed for with / (Composer).
 */
export function useScreenshot(onAdd: (files: File[]) => void) {
  const { t } = useTranslation()
  return async () => {
    try {
      // "2026-10-29 15.30.12": sortable, and no colon, which some systems refuse in a name.
      const stamp = new Date().toLocaleString('sv').replace(/:/g, '.')
      const file = await captureScreen(`${t('chat.attach.screenshot-name')} ${stamp}`)
      if (file) onAdd([file])
    } catch {
      toast.error(t('chat.attach.screenshot-failed'))
    }
  }
}
