import * as pdfjs from 'pdfjs-dist'
import type { PDFDocumentLoadingTask, PDFDocumentProxy, RenderTask } from 'pdfjs-dist'
import workerUrl from 'pdfjs-dist/build/pdf.worker.min.mjs?url'
import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'

pdfjs.GlobalWorkerOptions.workerSrc = workerUrl

/** How long the panel's width must hold before the pages are drawn again. */
const RESIZE_SETTLE_MS = 150

/** Where the build puts what pdf.js reads besides its code (vite.config.ts, pdfjsAssets). */
const ASSETS = `${import.meta.env.BASE_URL}pdfjs/`

/**
 * A PDF in the file panel, drawn by pdf.js page by page at the panel's width,
 * the same on every browser and phone - the browser's own viewer, in a frame,
 * shows only the first page or offers a download on some phones. Each page is
 * drawn as it comes near, and again when the panel is resized. Loaded only
 * when a PDF is opened (FilePanel's lazy import).
 */
export default function PdfView({ url }: { url: string }) {
  const { t } = useTranslation()
  const frame = useRef<HTMLDivElement>(null)
  const [doc, setDoc] = useState<PDFDocumentProxy | null>(null)
  const [failed, setFailed] = useState(false)
  const [ratio, setRatio] = useState(1.414)
  const [width, setWidth] = useState(0)

  useEffect(() => {
    const abort = new AbortController()
    let task: PDFDocumentLoadingTask | null = null
    let alive = true
    void (async () => {
      try {
        // Read whole, as the panel reads any file: a range request is a
        // header the bucket's CORS may not allow.
        const response = await fetch(url, { signal: abort.signal })
        if (!response.ok) throw new Error(`file ${response.status}`)
        const data = new Uint8Array(await response.arrayBuffer())
        if (!alive) return
        task = pdfjs.getDocument({
          data,
          cMapUrl: `${ASSETS}cmaps/`,
          cMapPacked: true,
          wasmUrl: `${ASSETS}wasm/`,
          useSystemFonts: true,
        })
        const loaded = await task.promise
        const first = await loaded.getPage(1)
        if (!alive) return
        const viewport = first.getViewport({ scale: 1 })
        setRatio(viewport.height / viewport.width)
        setDoc(loaded)
      } catch {
        if (alive) setFailed(true)
      }
    })()
    return () => {
      alive = false
      abort.abort()
      void task?.destroy()
    }
  }, [url])

  useEffect(() => {
    const outer = frame.current
    if (!outer) return
    // Drawn again only once a drag of the panel settles: in between, the
    // pages already drawn are scaled to the width (attachments CSS).
    let timer = 0
    const measure = () => setWidth(Math.max(0, outer.clientWidth - 32))
    measure()
    const observer = new ResizeObserver(() => {
      window.clearTimeout(timer)
      timer = window.setTimeout(measure, RESIZE_SETTLE_MS)
    })
    observer.observe(outer)
    return () => {
      observer.disconnect()
      window.clearTimeout(timer)
    }
  }, [])

  return (
    <div ref={frame} className="n-pdf-view">
      {failed ? (
        <p className="n-file-panel-note">{t('chat.file.load-failed')}</p>
      ) : !doc || width === 0 ? (
        <p className="n-file-panel-note">{t('chat.file.loading')}</p>
      ) : (
        Array.from({ length: doc.numPages }, (_, i) => (
          <PdfPage key={i} doc={doc} number={i + 1} width={width} ratio={ratio} label={t('chat.file.page', { n: i + 1, total: doc.numPages })} />
        ))
      )}
    </div>
  )
}

function PdfPage({ doc, number, width, ratio, label }: { doc: PDFDocumentProxy; number: number; width: number; ratio: number; label: string }) {
  const box = useRef<HTMLDivElement>(null)
  const canvas = useRef<HTMLCanvasElement>(null)
  const [near, setNear] = useState(false)
  const [height, setHeight] = useState(width * ratio)

  useEffect(() => {
    const element = box.current
    if (!element) return
    const observer = new IntersectionObserver(([entry]) => entry?.isIntersecting && setNear(true), { rootMargin: '800px 0px' })
    observer.observe(element)
    return () => observer.disconnect()
  }, [])

  useEffect(() => {
    const target = canvas.current
    if (!near || !target || width === 0) return
    let task: RenderTask | null = null
    let alive = true
    void (async () => {
      const page = await doc.getPage(number)
      if (!alive) return
      const base = page.getViewport({ scale: 1 })
      const scale = width / base.width
      const pixels = Math.min(window.devicePixelRatio || 1, 2)
      const viewport = page.getViewport({ scale: scale * pixels })
      target.width = Math.floor(viewport.width)
      target.height = Math.floor(viewport.height)
      setHeight(viewport.height / pixels)
      task = page.render({ canvas: target, viewport })
      await task.promise.catch(() => undefined)
    })()
    return () => {
      alive = false
      task?.cancel()
    }
  }, [doc, number, width, near])

  return (
    <div ref={box} className="n-pdf-page" style={{ minHeight: height }} aria-label={label} role="img">
      <canvas ref={canvas} />
    </div>
  )
}
