import { Code, Eye, Maximize2, X } from 'lucide-react'
import { useEffect, useId, useMemo, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import { useTranslation } from 'react-i18next'
import { CodeBlock, CodeBlockCopyButton, CodeBlockDownloadButton, type CustomRendererProps } from 'streamdown'

/** The preview's height when the code view could not be measured. */
const FALLBACK_HEIGHT = 320

/**
 * An html code block that can be seen as the page it describes.
 *
 * The page runs in an iframe sandboxed with scripts but without
 * `allow-same-origin`: it gets an opaque origin of its own, so it can draw and
 * run whatever a demo needs — CDN scripts included — but cannot read this
 * page, its storage, or call the API as the signed-in user. Nothing else is
 * granted; in particular it is never opened in a tab of its own, where it
 * would run as this origin.
 *
 * The preview takes exactly the room the code took — its body is measured
 * the moment the reader switches — so switching never moves the answer; a
 * page taller than that scrolls inside the frame.
 */
export function HtmlPreview({ code, isIncomplete, language }: CustomRendererProps) {
  const { t } = useTranslation()
  const [view, setView] = useState<'code' | 'preview'>('code')
  const [full, setFull] = useState(false)
  const block = useRef<HTMLDivElement>(null)
  const [height, setHeight] = useState(FALLBACK_HEIGHT)

  const flip = () => {
    if (view === 'code') {
      const body = block.current?.querySelector('[data-streamdown="code-block-body"]')
      if (body) setHeight(Math.round(body.getBoundingClientRect().height))
    }
    setView(view === 'code' ? 'preview' : 'code')
  }

  const toggle = (
    <button
      type="button"
      className="n-code-action"
      disabled={isIncomplete}
      aria-pressed={view === 'preview'}
      onClick={flip}
    >
      {view === 'code' ? <Eye aria-hidden="true" /> : <Code aria-hidden="true" />}
      <span>{view === 'code' ? t('chat.preview.show') : t('chat.preview.code')}</span>
    </button>
  )

  if (view === 'code' || isIncomplete) {
    return (
      <div ref={block}>
        <CodeBlock code={code} language={language} isIncomplete={isIncomplete}>
          {toggle}
          <CodeBlockDownloadButton />
          <CodeBlockCopyButton />
        </CodeBlock>
      </div>
    )
  }

  return (
    <div data-streamdown="code-block" className="n-html-preview">
      <div data-streamdown="code-block-header">{language}</div>
      <div className="n-code-actions-row">
        <div data-streamdown="code-block-actions">
          {toggle}
          <button type="button" className="n-code-action is-icon" aria-label={t('chat.preview.fullscreen')} onClick={() => setFull(true)}>
            <Maximize2 aria-hidden="true" />
          </button>
        </div>
      </div>
      {!full && <PreviewFrame html={code} height={height} />}
      {/* Portalled to the chat's root: an older message is laid out with
          containment, which would trap a fixed layer inside its own box. */}
      {full &&
        createPortal(
          <div className="n-html-full" role="dialog" aria-modal="true" aria-label={t('chat.preview.fullscreen')}>
            <div className="n-html-full-bar">
              <span>{t('chat.preview.title')}</span>
              <button type="button" className="n-code-action is-icon" aria-label={t('chat.preview.close')} autoFocus onClick={() => setFull(false)}>
                <X aria-hidden="true" />
              </button>
            </div>
            <PreviewFrame html={code} onEscape={() => setFull(false)} />
          </div>,
          document.querySelector('.prism-next') ?? document.body,
        )}
    </div>
  )
}

function PreviewFrame({ html, height, onEscape }: { html: string; height?: number; onEscape?: () => void }) {
  const { t } = useTranslation()
  const frame = useRef<HTMLIFrameElement>(null)
  const token = useId()
  const srcDoc = useMemo(() => withEscape(html, token), [html, token])

  // Escape closes the full-screen view, pressed here or inside the page.
  useEffect(() => {
    if (!onEscape) return
    const onMessage = (event: MessageEvent) => {
      if (event.source !== frame.current?.contentWindow) return
      const data = event.data as { openchatPreview?: string; escape?: boolean } | null
      if (data?.openchatPreview === token && data.escape) onEscape()
    }
    const onKey = (event: KeyboardEvent) => {
      if (event.key === 'Escape') onEscape()
    }
    window.addEventListener('message', onMessage)
    window.addEventListener('keydown', onKey)
    return () => {
      window.removeEventListener('message', onMessage)
      window.removeEventListener('keydown', onKey)
    }
  }, [onEscape, token])

  return (
    <iframe
      ref={frame}
      className="n-html-frame"
      title={t('chat.preview.title')}
      sandbox="allow-scripts allow-forms allow-modals allow-popups"
      referrerPolicy="no-referrer"
      srcDoc={srcDoc}
      style={height ? { height } : undefined}
    />
  )
}

/**
 * The page plus a line that passes Escape up: keys pressed inside the frame
 * never reach this document. Only a message from the frame's own window, with
 * its token, is heard.
 */
function withEscape(html: string, token: string): string {
  const script = `<script>addEventListener('keydown',e=>{if(e.key==='Escape')parent.postMessage({openchatPreview:${JSON.stringify(token)},escape:true},'*')})</script>`
  const end = html.search(/<\/body\s*>/i)
  return end >= 0 ? html.slice(0, end) + script + html.slice(end) : html + script
}

export default HtmlPreview
