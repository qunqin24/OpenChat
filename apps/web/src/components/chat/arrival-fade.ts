import { useLayoutEffect, useRef, type RefObject } from 'react'

import { prefersReducedMotion } from '@/lib/motion'
import { coverOpacity, nextArrivals, noArrivals, type ArrivalBatch as Batch, type ArrivalState } from '@openchat/chat-core/arrival'

/**
 * Text fades in as it arrives: whatever one render adds is one batch, laid out
 * at once and swept in top to bottom at the speed text has been coming, a
 * piece at a time, each piece fading in over ARRIVAL_FADE_MS
 * (chat-core/arrival.ts).
 *
 * Nothing in the rendered text is touched. A layer over it (`layer`, a sibling
 * of `content` inside a positioned box) holds a cover per line of each piece
 * still to come or fading, in the colour of the surface behind that text,
 * opaque until the piece is due and then going clear: on a plain surface that
 * is the text's own fade. So the elements are the lines of the last
 * MAX_CATCH_UP_MS + ARRIVAL_FADE_MS at most, however long the answer - unlike
 * v1.1.0's Streamdown `animated`, which gave every character a fading element
 * and never took one back, and took long reasoning to 90% of a core - and
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
  const firsts = new Map<Element, Text | null>()
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
      const marker = text.data.slice(0, a).trim() ? null : markerLeft(text, firsts)
      for (const [i, rect] of [...range.getClientRects()].entries()) {
        if (rect.width === 0 || rect.height === 0) continue
        const left = Math.floor((i === 0 && marker !== null ? Math.min(marker, rect.left) : rect.left) - origin.left)
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

/**
 * Where the marker of the list item `text` begins, if `text` is the item's
 * first text, so the cover over its start covers the number or bullet too: a
 * marker is not text, and left bare it would show before its item. A number
 * hangs out of the list's left edge once it has two digits, so the left is
 * reckoned from its length. Each item's first text is remembered.
 */
function markerLeft(text: Text, firsts: Map<Element, Text | null>): number | null {
  const item = text.parentElement?.closest('li')
  if (!item?.parentElement) return null
  let first = firsts.get(item)
  if (first === undefined) {
    const walker = document.createTreeWalker(item, NodeFilter.SHOW_TEXT, {
      acceptNode: (node) => ((node as Text).data.trim() ? NodeFilter.FILTER_ACCEPT : NodeFilter.FILTER_SKIP),
    })
    first = walker.nextNode() as Text | null
    firsts.set(item, first)
  }
  if (first !== text) return null
  const list = item.parentElement
  const number = list instanceof HTMLOListElement ? list.start + [...list.children].indexOf(item) : null
  const chars = number === null ? 1 : `${number}.`.length
  const em = parseFloat(getComputedStyle(item).fontSize) || 16
  return Math.min(list.getBoundingClientRect().left, item.getBoundingClientRect().left - (chars * 0.6 + 0.6) * em)
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
