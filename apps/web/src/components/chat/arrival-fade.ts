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
 * Where a batch is is counted in characters of `content`'s text, after every
 * render: what was there stays where it was as Markdown grows, so the new
 * characters are the last ones (a formula or diagram drawn once its code
 * arrives grows the text too, and fades in the same way). What the box holds
 * when it mounts is shown as it is unless
 * `fresh` (it arrived after the reader was watching). Text cut short, a hidden
 * tab, and reduced motion get no fade.
 */
export function useArrivalFade(content: RefObject<HTMLElement | null>, layer: RefObject<HTMLElement | null>, fresh = false) {
  // What counts as a batch, and how its covers fade, are shared with the app (chat-core/arrival.ts).
  const arrivals = useRef<ArrivalState>(noArrivals)
  const covers = useRef<{ el: HTMLElement; born: number }[]>([])
  const frame = useRef(0)

  useLayoutEffect(() => {
    const root = content.current
    const over = layer.current
    if (!root || !over) return
    const length = root.textContent?.length ?? 0
    arrivals.current = nextArrivals(arrivals.current, length, performance.now(), {
      fresh,
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
 */
function place(root: HTMLElement, layer: HTMLElement, batches: Batch[]): { el: HTMLElement; born: number }[] {
  const placed: { el: HTMLElement; born: number }[] = []
  if (batches.length > 0) {
    const from = Math.min(...batches.map((batch) => batch.start))
    const origin = layer.getBoundingClientRect()
    const surfaces = new Map<Element, string>()
    const pool = [...layer.children] as HTMLElement[]
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
    for (const batch of batches) {
      for (const { node: text, start } of nodes) {
        const a = Math.max(batch.start, start) - start
        const b = Math.min(batch.end, start + text.length) - start
        if (a >= b || !text.data.slice(a, b).trim()) continue
        const parent = text.parentElement
        if (!parent) continue
        let surface = surfaces.get(parent)
        if (surface === undefined) {
          surface = surfaceOf(parent)
          surfaces.set(parent, surface)
        }
        const range = document.createRange()
        range.setStart(text, a)
        range.setEnd(text, b)
        for (const rect of range.getClientRects()) {
          if (rect.width === 0 || rect.height === 0) continue
          const el = pool.pop() ?? layer.appendChild(document.createElement('span'))
          const left = Math.floor(rect.left - origin.left)
          const top = Math.floor(rect.top - origin.top)
          el.style.transform = `translate(${left}px, ${top}px)`
          el.style.width = `${Math.ceil(rect.right - origin.left) - left}px`
          el.style.height = `${Math.ceil(rect.bottom - origin.top) - top}px`
          el.style.background = surface
          placed.push({ el, born: batch.born })
        }
      }
    }
    for (const spare of pool) spare.remove()
  } else {
    layer.replaceChildren()
  }
  return placed
}

/** The colour behind `element`: its own background, or the nearest one above it. */
function surfaceOf(element: Element): string {
  for (let at: Element | null = element; at; at = at.parentElement) {
    const color = getComputedStyle(at).backgroundColor
    if (color && color !== 'transparent' && !/rgba\(.*,\s*0\)$/.test(color)) return color
  }
  return 'var(--n-bg)'
}
