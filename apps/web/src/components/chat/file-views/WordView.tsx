import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'

// An A4 page and Word's default margins, in CSS pixels (96 to the inch).
const A4_WIDTH = '794px'
const A4_HEIGHT = '1123px'
const A4_MARGINS = '96px 120px'

/**
 * A Word file in the file panel, laid out as Word lays it out - fonts,
 * colours, tables, pictures, pages - by docx-preview, from the file itself.
 * The pages keep their own width and are scaled down to fit the panel, as an
 * office app zooms to fit, rather than reflowed. Loaded only when a Word file
 * is opened (FilePanel's lazy import).
 */
export default function WordView({ url }: { url: string }) {
  const { t } = useTranslation()
  const frame = useRef<HTMLDivElement>(null)
  const body = useRef<HTMLDivElement>(null)
  const [state, setState] = useState<'loading' | 'ready' | 'error'>('loading')

  useEffect(() => {
    const target = body.current
    if (!target) return
    const abort = new AbortController()
    let alive = true
    void (async () => {
      try {
        const response = await fetch(url, { signal: abort.signal })
        if (!response.ok) throw new Error(`file ${response.status}`)
        const data = await response.blob()
        const { renderAsync } = await import('docx-preview')
        if (!alive) return
        target.replaceChildren()
        await renderAsync(data, target, target, {
          inWrapper: true,
          breakPages: true,
          useBase64URL: true,
          renderComments: false,
          renderChanges: false,
        })
        if (!alive) return
        // A file that says nothing of its page - many converters write none -
        // gets the page an office app gives it: A4, 2.54 cm above and below,
        // 3.18 cm at the sides. docx-preview would run the text from edge to
        // edge of the panel instead.
        for (const page of target.querySelectorAll<HTMLElement>('section.docx')) {
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

  // Zoom to fit: the widest page against the panel's width, never above 1.
  useEffect(() => {
    const outer = frame.current
    if (state !== 'ready' || !outer) return
    const fit = () => {
      const wrapper = outer.querySelector<HTMLElement>('.docx-wrapper')
      if (!wrapper) return
      wrapper.style.zoom = '1'
      const pages = [...wrapper.querySelectorAll<HTMLElement>('section.docx')]
      const widest = Math.max(...pages.map((page) => page.offsetWidth), 1)
      wrapper.style.zoom = String(Math.min(1, (outer.clientWidth - 32) / widest))
    }
    fit()
    // Fitted again once a drag of the panel settles, not on every frame of it.
    let timer = 0
    const observer = new ResizeObserver(() => {
      window.clearTimeout(timer)
      timer = window.setTimeout(fit, 120)
    })
    observer.observe(outer)
    return () => {
      observer.disconnect()
      window.clearTimeout(timer)
    }
  }, [state])

  return (
    <div ref={frame} className="n-word-view">
      {state === 'loading' && <p className="n-file-panel-note">{t('chat.file.loading')}</p>}
      {state === 'error' && <p className="n-file-panel-note">{t('chat.file.load-failed')}</p>}
      <div ref={body} hidden={state !== 'ready'} />
    </div>
  )
}
