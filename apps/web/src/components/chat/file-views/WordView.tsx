import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'

// An A4 page and Word's default margins, in CSS pixels (96 to the inch).
const A4_WIDTH = '794px'
const A4_HEIGHT = '1123px'
const A4_MARGINS = '96px 120px'

/**
 * The frame the pages are drawn in. A Word file is someone else's content, so
 * it never shares the page with the app: the frame may not run scripts (no
 * `allow-scripts`, so nothing in it, nor in any frame it holds, can run), and
 * its styles stay inside it. `allow-same-origin` only lets this component
 * reach in to draw; `allow-popups` lets a web link open in a new tab, outside
 * the sandbox.
 */
const FRAME_SANDBOX = 'allow-same-origin allow-popups allow-popups-to-escape-sandbox'
const FRAME_DOC = `<!doctype html><html><head><meta charset="utf-8"><style>
html, body { margin: 0; background: transparent; }
.docx-wrapper { padding: 0 !important; background: transparent !important; }
.docx-wrapper > section.docx { margin: 0 auto 16px !important; box-shadow: 0 1px 4px rgb(0 0 0 / 10%); }
</style></head><body><div id="styles"></div><div id="pages"></div></body></html>`

/** What a link in a Word file may still point at: the web, mail, or a place in the file. */
const SAFE_LINK = /^(https?:|mailto:|#)/i

/**
 * The frame's two boxes once it has loaded its page: one for docx-preview's
 * styles (it empties the box it writes them to, so not the head, which holds
 * the frame's own), one for the pages.
 */
function frameBoxes(frame: HTMLIFrameElement): Promise<{ doc: Document; styles: HTMLElement; pages: HTMLElement }> {
  const boxes = () => {
    const doc = frame.contentDocument
    const styles = doc?.getElementById('styles')
    const pages = doc?.getElementById('pages')
    return doc && styles && pages && doc.readyState === 'complete' ? { doc, styles, pages } : null
  }
  return new Promise((resolve, reject) => {
    const now = boxes()
    if (now) return resolve(now)
    frame.addEventListener('load', () => {
      const loaded = boxes()
      if (loaded) resolve(loaded)
      else reject(new Error('frame not ready'))
    }, { once: true })
  })
}

/**
 * Whatever docx-preview drew that is not text and pictures goes: frames,
 * scripts, embedded objects, forms. Links keep only the web, mail and places
 * in the file, and open in a new tab.
 */
function tidy(doc: Document) {
  for (const element of doc.body.querySelectorAll('script, iframe, frame, object, embed, form, base, meta, link')) {
    element.remove()
  }
  for (const link of doc.body.querySelectorAll('a[href]')) {
    const href = link.getAttribute('href') ?? ''
    if (!SAFE_LINK.test(href.trim())) {
      link.removeAttribute('href')
      continue
    }
    if (!href.startsWith('#')) {
      link.setAttribute('target', '_blank')
      link.setAttribute('rel', 'noopener noreferrer')
    }
  }
}

/**
 * A Word file in the file panel, laid out as Word lays it out - fonts,
 * colours, tables, pictures, pages - by docx-preview, from the file itself,
 * inside a sandboxed frame (FRAME_SANDBOX). The pages keep their own width and
 * are scaled down to fit the panel, as an office app zooms to fit, rather than
 * reflowed; the frame is as tall as the pages, so the panel scrolls them.
 * Loaded only when a Word file is opened (FilePanel's lazy import).
 */
export default function WordView({ url }: { url: string }) {
  const { t } = useTranslation()
  const outer = useRef<HTMLDivElement>(null)
  const frame = useRef<HTMLIFrameElement>(null)
  const [state, setState] = useState<'loading' | 'ready' | 'error'>('loading')

  useEffect(() => {
    const target = frame.current
    if (!target) return
    const abort = new AbortController()
    let alive = true
    void (async () => {
      try {
        const [response, { doc, styles, pages }] = await Promise.all([
          fetch(url, { signal: abort.signal }),
          frameBoxes(target),
        ])
        if (!response.ok) throw new Error(`file ${response.status}`)
        const data = await response.blob()
        const { renderAsync } = await import('docx-preview')
        if (!alive) return
        await renderAsync(data, pages, styles, {
          inWrapper: true,
          breakPages: true,
          useBase64URL: true,
          renderComments: false,
          renderChanges: false,
          // Embedded HTML ("altChunk") would be drawn in a frame of its own.
          renderAltChunks: false,
        })
        if (!alive) return
        tidy(doc)
        // A file that says nothing of its page - many converters write none -
        // gets the page an office app gives it: A4, 2.54 cm above and below,
        // 3.18 cm at the sides. docx-preview would run the text from edge to
        // edge of the panel instead.
        for (const page of doc.body.querySelectorAll<HTMLElement>('section.docx')) {
          if (page.style.width) continue
          page.style.width = A4_WIDTH
          page.style.minHeight = A4_HEIGHT
          page.style.padding = A4_MARGINS
        }
        setState('ready')
      } catch {
        if (alive) setState('error')
      }
    })()
    return () => {
      alive = false
      abort.abort()
    }
  }, [url])

  // Zoom to fit: the widest page against the panel's width, never above 1;
  // then the frame takes the pages' height.
  useEffect(() => {
    const box = outer.current
    const target = frame.current
    if (state !== 'ready' || !box || !target) return
    const fit = () => {
      const doc = target.contentDocument
      const wrapper = doc?.querySelector<HTMLElement>('.docx-wrapper')
      if (!doc || !wrapper) return
      wrapper.style.zoom = '1'
      const pages = [...wrapper.querySelectorAll<HTMLElement>('section.docx')]
      const widest = Math.max(...pages.map((page) => page.offsetWidth), 1)
      wrapper.style.zoom = String(Math.min(1, box.clientWidth / widest))
      target.style.height = `${doc.documentElement.scrollHeight}px`
    }
    fit()
    // Fitted again once a drag of the panel settles, not on every frame of it.
    let timer = 0
    const observer = new ResizeObserver(() => {
      window.clearTimeout(timer)
      timer = window.setTimeout(fit, 120)
    })
    observer.observe(box)
    return () => {
      observer.disconnect()
      window.clearTimeout(timer)
    }
  }, [state])

  return (
    <div className="n-word-view">
      {state === 'loading' && <p className="n-file-panel-note">{t('chat.file.loading')}</p>}
      {state === 'error' && <p className="n-file-panel-note">{t('chat.file.load-failed')}</p>}
      <div ref={outer} className="n-word-pages" hidden={state === 'error'}>
        <iframe
          ref={frame}
          className="n-word-frame"
          title={t('chat.file.word-pages')}
          sandbox={FRAME_SANDBOX}
          srcDoc={FRAME_DOC}
          data-ready={state === 'ready' || undefined}
        />
      </div>
    </div>
  )
}
