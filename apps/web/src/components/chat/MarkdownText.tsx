import { useEffect, useLayoutEffect, useRef, useState } from 'react'
import { Streamdown } from 'streamdown'

import { prefersReducedMotion } from '@/lib/motion'
import { revealDone, revealed, revealStep, type RevealState } from '@/lib/reveal'
import { markdownComponents, markdownLinkSafety, prepareMarkdown, useMarkdownPlugins } from './markdown-components'

/** Markdown as the chat draws it: an answer's text, the reasoning, a conversation's summary. */
export function Markdown({ text, streaming = false }: { text: string; streaming?: boolean }) {
  const shown = useReveal(text)
  const behind = shown.length < text.length
  const revealing = streaming || behind
  const plugins = useMarkdownPlugins(shown)
  const box = useRef<HTMLDivElement>(null)
  const tail = useRef<HTMLSpanElement>(null)
  // After every render: the fade sits on the newest characters while any are
  // still being revealed.
  useLayoutEffect(() => placeTail(box.current, tail.current, behind))
  return (
    <div className="n-markdown" ref={box}>
      {/* The local typography layer styles the actual Streamdown output;
          markdown-body keeps the ported table selectors working. */}
      <div className="markdown-body">
        <Streamdown className="n-prose" parseIncompleteMarkdown={revealing} isAnimating={revealing} components={markdownComponents} plugins={plugins} linkSafety={markdownLinkSafety}>
          {prepareMarkdown(shown)}
        </Streamdown>
      </div>
      <span ref={tail} className="n-reveal-tail" aria-hidden="true" />
    </div>
  )
}

/** How far back from the newest character the fade reaches, in em. */
const TAIL_EM = 2.5

/**
 * The newest characters fade in: a short gradient from clear to the page's
 * colour lies over the end of the last line, so the character just shown is
 * mostly hidden and each one before it a little less, and as the reveal moves
 * on every character comes out from under it. One element, placed once a
 * render - the cost does not grow with the answer, unlike a fading element per
 * character (v1.1.0, which took long reasoning to 90% of a core). Text inside a
 * code block, table, diagram or interface block has a surface of its own and
 * gets no fade.
 */
function placeTail(box: HTMLElement | null, tail: HTMLElement | null, on: boolean) {
  if (!box || !tail) return
  const hide = () => {
    tail.style.opacity = '0'
  }
  if (!on) return hide()
  const node = lastText(box.firstElementChild)
  const parent = node?.parentElement
  if (!node || !parent || parent.closest('pre, table, .n-ui, .n-mermaid, [data-streamdown="code-block"]')) return hide()
  const end = node.data.trimEnd().length
  const range = document.createRange()
  range.setStart(node, Math.max(0, end - 1))
  range.setEnd(node, end)
  const rects = range.getClientRects()
  const rect = rects[rects.length - 1]
  if (!rect || rect.height === 0) return hide()
  const origin = box.getBoundingClientRect()
  const width = TAIL_EM * parseFloat(getComputedStyle(parent).fontSize)
  const left = Math.max(0, rect.right - origin.left - width)
  tail.style.width = `${rect.right - origin.left - left}px`
  tail.style.height = `${rect.height}px`
  tail.style.transform = `translate(${left}px, ${rect.top - origin.top}px)`
  tail.style.setProperty('--n-tail-bg', surfaceOf(parent))
  tail.style.opacity = '1'
}

/** The last text node under `root` with anything but whitespace in it. */
function lastText(root: Node | null): Text | null {
  let node: Node | null = root?.lastChild ?? null
  while (node) {
    if (node.nodeType === Node.TEXT_NODE && node.textContent?.trim()) return node as Text
    if (node.nodeType === Node.ELEMENT_NODE && node.lastChild) {
      node = node.lastChild
      continue
    }
    while (node && node !== root && !node.previousSibling) node = node.parentNode
    if (!node || node === root) return null
    node = node.previousSibling
  }
  return null
}

/** The colour behind `element`: its own background, or the nearest one above it. */
function surfaceOf(element: Element): string {
  for (let at: Element | null = element; at; at = at.parentElement) {
    const color = getComputedStyle(at).backgroundColor
    if (color && color !== 'transparent' && !/rgba\(.*,\s*0\)$/.test(color)) return color
  }
  return 'var(--n-bg)'
}

/**
 * The least time between two renders of a reveal, in ms: about 30 a second.
 * Each render parses the text shown as Markdown, which costs more the longer
 * it is; every frame (60 a second or more) made a long reasoning cost a
 * quarter of a core, for no step a reader could tell apart.
 */
const REVEAL_FRAME_MS = 32

/**
 * The part of `text` to show (lib/reveal.ts). What the element holds when it
 * mounts is shown at once - a page opened, or reloaded, part-way through an
 * answer does not type out what was already there - and what is added after is
 * revealed at a pace, including after the stream has ended: the element is the
 * same one across the hand-over (the part keys in `AssistantTurn`), so the end
 * of an answer finishes typing rather than jumping in. Text that is not a
 * continuation (replaced, shortened) is shown as it is. A hidden tab, and a
 * reader who asked for reduced motion, get the text as it is.
 */
function useReveal(text: string): string {
  const [shown, setShown] = useState(text.length)
  // The text as last rendered, to tell a continuation from a replacement.
  const [held, setHeld] = useState(text)
  if (held !== text) {
    setHeld(text)
    if (!text.startsWith(revealed(held, shown))) setShown(text.length)
  }
  // What the frame loop reads: the latest text, and where the reveal stands.
  // The loop's position is fractional and runs ahead of the rendered whole
  // characters; a render moves it only forward (text shown at once) or back to
  // the text's end (text cut short).
  const target = useRef(text)
  const state = useRef<RevealState>(revealDone(text.length))
  useLayoutEffect(() => {
    target.current = text
    state.current.shown = Math.min(Math.max(state.current.shown, shown), text.length)
  }, [text, shown])
  const behind = shown < text.length

  useEffect(() => {
    // Caught up: the arrival rate is kept, for the pace of what comes next.
    if (!behind) {
      state.current = revealDone(target.current.length, state.current.rate)
      return
    }
    if (prefersReducedMotion() || document.hidden) {
      state.current = revealDone(target.current.length)
      setShown(target.current.length)
      return
    }
    let frame = 0
    let last = performance.now()
    let drawn = last
    const tick = (now: number) => {
      const next = revealStep(state.current, target.current.length, now - last)
      last = now
      state.current = next
      const done = next.shown >= target.current.length
      if (done || now - drawn >= REVEAL_FRAME_MS || document.hidden) {
        drawn = now
        // Whole characters: a render only when one more can be shown.
        setShown(document.hidden ? target.current.length : Math.floor(next.shown))
      }
      if (!done && !document.hidden) frame = requestAnimationFrame(tick)
    }
    frame = requestAnimationFrame(tick)
    return () => cancelAnimationFrame(frame)
  }, [behind])

  return revealed(text, shown)
}
