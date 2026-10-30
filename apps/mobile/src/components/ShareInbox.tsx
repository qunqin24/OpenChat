import { getInfoAsync } from 'expo-file-system/legacy'
import { router } from 'expo-router'
import { useShareIntentContext, type ShareIntent } from 'expo-share-intent'
import { useEffect } from 'react'

import { useShared, type Shared } from '@/lib/shared'

/**
 * Takes what another app shared - text, a link, pictures, files - and opens a
 * new chat for it (lib/shared.ts). Mounted with the signed-in screens only: a
 * share that arrives signed out waits in the share-intent provider until the
 * person has signed in. Cold start and a share into the running app alike.
 */
export function ShareInbox() {
  const { hasShareIntent, shareIntent, resetShareIntent } = useShareIntentContext()
  const put = useShared((s) => s.put)

  useEffect(() => {
    if (!hasShareIntent) return
    let cancelled = false
    void sharedOf(shareIntent).then((shared) => {
      if (cancelled) return
      resetShareIntent()
      if (!shared.text && shared.files.length === 0) return
      put(shared)
      router.navigate({ pathname: '/', params: { n: String(Date.now()) } })
    })
    return () => {
      cancelled = true
    }
  }, [hasShareIntent, shareIntent, resetShareIntent, put])

  return null
}

async function sharedOf(intent: ShareIntent): Promise<Shared> {
  const text = [intent.text, intent.webUrl && !intent.text?.includes(intent.webUrl) ? intent.webUrl : null].filter(Boolean).join('\n')
  const files = await Promise.all(
    (intent.files ?? []).map(async (file) => {
      const uri = /^[a-z]+:\/\//i.test(file.path) ? file.path : `file://${file.path}`
      let size = file.size ?? 0
      if (!size) {
        const info = await getInfoAsync(uri).catch(() => null)
        if (info?.exists) size = info.size
      }
      return {
        uri,
        // A sharing app may give no name or type; the extension, if any, is what is judged.
        name: file.fileName || 'shared-file',
        type: file.mimeType || '',
        size,
        ...(file.width ? { width: file.width } : {}),
        ...(file.height ? { height: file.height } : {}),
      }
    }),
  )
  return { text, files }
}
