import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useColorScheme } from 'react-native'

import { toast } from '@/lib/toast'
import { useColors } from '@/theme'
import { mermaidSource } from '@/vendor/mermaid'
import { PagePreview } from './PagePreview'

/**
 * A mermaid block drawn as its diagram, full screen and zoomable (the web's
 * MermaidDiagram). Mermaid runs in the preview's WebView with the web's
 * settings - no scripts, no HTML in labels, the SVG sanitised, since the source
 * is the model's - and says whether it could draw.
 */
export function MermaidPreview({ code, onClose }: { code: string; onClose: () => void }) {
  const { t } = useTranslation()
  const colors = useColors()
  const dark = useColorScheme() === 'dark'
  const [failed, setFailed] = useState(false)
  // Mermaid sits inline in the page (the vendoring script makes sure it can):
  // handed over separately, before the page, its 3.5 MB crashed the WebView.
  // The diagram's source goes in as a JSON string with `<` escaped, so it can
  // never close the script it sits in.
  const html = useMemo(
    () => `<!doctype html><html><head>
<meta name="viewport" content="width=device-width, initial-scale=1, maximum-scale=6">
<style>body{margin:0;padding:16px;background:${colors.bg};color:${colors.text};font-family:sans-serif}#out svg{max-width:100%;height:auto}</style>
</head><body><div id="out"></div><script>${mermaidSource()}</script><script>
(async () => {
  const code = ${JSON.stringify(code).replace(/</g, '\\u003c')}
  try {
    mermaid.initialize({ startOnLoad: false, securityLevel: 'strict', suppressErrorRendering: true, theme: ${JSON.stringify(dark ? 'dark' : 'neutral')} })
    await mermaid.parse(code)
    document.getElementById('out').innerHTML = (await mermaid.render('diagram', code)).svg
    window.ReactNativeWebView.postMessage('drawn')
  } catch (error) {
    window.ReactNativeWebView.postMessage('failed')
  }
})()
</script></body></html>`,
    [code, colors.bg, colors.text, dark],
  )
  if (failed) return null
  return (
    <PagePreview
      title={t('chat.diagram.label')}
      html={html}
      onMessage={(event) => {
        if (event.nativeEvent.data !== 'failed') return
        setFailed(true)
        toast.error(t('chat.diagram.invalid'))
        onClose()
      }}
      onClose={onClose}
    />
  )
}
