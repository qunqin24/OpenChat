import { useLayoutEffect, useRef, type RefObject } from 'react'

import { prefersReducedMotion } from '@/lib/motion'
import { coverOpacity, nextArrivals, noArrivals, type ArrivalBatch as Batch, type ArrivalState } from '@openchat/chat-core/arrival'

/**
 * Text fades in a batch at a time, as it arrives: whatever one render adds is
 * one batch, shown at once and faded in over ARRIVAL_FADE_MS together, the
 * way it came.
 *
 * Nothing in the rendered text is touched. A layer over it (`layer`, a sibling
 * of `content` inside a positioned box) holds a cover per line of each batch
 * still fading, in the colour of the surface behind that text, going from
 * opaque to clear: on a plain surface that is the text's own fade. So the
 * elements are the lines of the last ARRIVAL_FADE_MS, a handful, however long
 * the answer - unlike a fading element per character or word (v1.1.0's
 * Streamdown `animated`, which took long reasoning to 90% of a core), and
 * React keeps the text it rendered.
 *
 * Where a batch is is counted in characters of `content`'s text: what was
 * there stays where it was as Markdown grows, so the new characters are the
 * last ones. Only a render with new `text` makes a batch. The rendered text
 * also grows on its own - a diagram or formula drawn after its source, code
 * highlighted - and that is taken in silently: a diagram drawn while the
 * reader scrolled once became one batch of hundreds of labels, measured with
 * a style recalculation per label, and froze the page for seconds (v1.3.1).
 * Text inside a diagram or a formula is never covered, for the same reason.
 *
 * What the box holds when it mounts is shown as it is unless `fresh` (it
 * arrived after the reader was watching). Text cut short, a batch over
 * MAX_BATCH_CHARS, a hidden tab and reduced motion get no fade. What counts
 * as a batch, and how its covers fade, are shared with the app
 * (chat-core/arrival.ts).
 */
export function useArrivalFade(content: RefObject<HTMLElement | null>, layer: RefObject<HTMLElement | null>, text: string, fresh = false) {
  const arrivals = useRef<ArrivalState>(noArrivals)
  const seen = useRef<string | null>(null)
  const covers = useRef<{ el: HTMLElement; born: number }[]>([])
  const frame = useRef(0)

  useLayoutEffect(() => {
    const root = content.current
    const over = layer.current
    if (!root || !over) return
    const length = root.textContent?.length ?? 0
    const arrived = text !== seen.current
    seen.current = text
    arrivals.current = nextArrivals(arrivals.current, length, performance.now(), {
      fresh,
      arrived,
      fading: !prefersReducedMotion() && !document.hidden,
    })
    covers.current = place(root, over, arrivals.current.batches)
    if (covers.current.length > 0 && !frame.current) {
      const tick = () => {
        const at = performance.now()
        let live = false
        for (const cover of covers.current) {
          const opacity = coverOpacity(cover.born, at)
          cover.el.style.opacity = String(opacity)
          if (opacity > 0) live = true
        }
        if (live) {
          frame.current = requestAnimationFrame(tick)
          return
        }
        frame.current = 0
        arrivals.current = { ...arrivals.current, batches: [] }
        over.replaceChildren()
        covers.current = []
      }
      tick()
    }
  })

  useLayoutEffect(
    () => () => {
      cancelAnimationFrame(frame.current)
      frame.current = 0
    },
    [],
  )
}

/**
 * Puts a cover over every line of every batch, reusing the layer's elements.
 * Walks the text from its end, only as far back as the oldest batch begins.
 *
 * Everything is measured before anything is written: a cover added between
 * two measurements makes the next one recalculate the page's style.
 */
function place(root: HTMLElement, layer: HTMLElement, batches: Batch[]): { el: HTMLElement; born: number }[] {
  if (batches.length === 0) {
    layer.replaceChildren()
    return []
  }
  const from = Math.min(...batches.map((batch) => batch.start))
  const origin = layer.getBoundingClientRect()
  const surfaces = new Map<Element, string>()
  let end = root.textContent?.length ?? 0
  const walker = document.createTreeWalker(root, NodeFilter.SHOW_TEXT)
  const nodes: { node: Text; start: number }[] = []
  // Elements are skipped, not rejected, so this is the last text node at any depth.
  let node = walker.lastChild() as Text | null
  while (node && end > from) {
    end -= node.length
    nodes.push({ node, start: end })
    node = walker.previousNode() as Text | null
  }
  const boxes: { left: number; top: number; width: number; height: number; surface: string; born: number }[] = []
  for (const batch of batches) {
    for (const { node: text, start } of nodes) {
      const a = Math.max(batch.start, start) - start
      const b = Math.min(batch.end, start + text.length) - start
      if (a >= b || !text.data.slice(a, b).trim()) continue
      const parent = text.parentElement
      if (!parent || parent.closest('svg, .katex')) continue
      const surface = surfaceOf(parent, surfaces)
      const range = document.createRange()
      range.setStart(text, a)
      range.setEnd(text, b)
      for (const rect of range.getClientRects()) {
        if (rect.width === 0 || rect.height === 0) continue
        const left = Math.floor(rect.left - origin.left)
        const top = Math.floor(rect.top - origin.top)
        boxes.push({ left, top, width: Math.ceil(rect.right - origin.left) - left, height: Math.ceil(rect.bottom - origin.top) - top, surface, born: batch.born })
      }
    }
  }
  const pool = [...layer.children] as HTMLElement[]
  const placed = boxes.map((box) => {
    const el = pool.pop() ?? layer.appendChild(document.createElement('span'))
    el.style.transform = `translate(${box.left}px, ${box.top}px)`
    el.style.width = `${box.width}px`
    el.style.height = `${box.height}px`
    el.style.background = box.surface
    return { el, born: box.born }
  })
  for (const spare of pool) spare.remove()
  return placed
}

/** The colour behind `element`: its own background, or the nearest one above it. Remembered per element. */
function surfaceOf(element: Element, known: Map<Element, string>): string {
  const path: Element[] = []
  let color = 'var(--n-bg)'
  for (let at: Element | null = element; at; at = at.parentElement) {
    const remembered = known.get(at)
    if (remembered !== undefined) {
      color = remembered
      break
    }
    path.push(at)
    const own = getComputedStyle(at).backgroundColor
    if (own && own !== 'transparent' && !/rgba\(.*,\s*0\)$/.test(own)) {
      color = own
      break
    }
  }
  for (const at of path) known.set(at, color)
  return color
}
