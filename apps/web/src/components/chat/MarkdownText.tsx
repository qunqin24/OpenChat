import { useEffect, useLayoutEffect, useRef, useState } from 'react'
import { Streamdown } from 'streamdown'

import { prefersReducedMotion } from '@/lib/motion'
import { revealDone, revealed, revealStep, type RevealState } from '@/lib/reveal'
import { markdownComponents, markdownLinkSafety, prepareMarkdown, useMarkdownPlugins } from './markdown-components'

/** Markdown as the chat draws it: an answer's text, the reasoning, a conversation's summary. */
export function Markdown({ text, streaming = false }: { text: string; streaming?: boolean }) {
  const shown = useReveal(text)
  const revealing = streaming || shown.length < text.length
  const plugins = useMarkdownPlugins(shown)
  return (
    <div className="n-markdown">
      {/* The local typography layer styles the actual Streamdown output;
          markdown-body keeps the ported table selectors working. */}
      <div className="markdown-body">
        <Streamdown className="n-prose" parseIncompleteMarkdown={revealing} isAnimating={revealing} components={markdownComponents} plugins={plugins} linkSafety={markdownLinkSafety}>
          {prepareMarkdown(shown)}
        </Streamdown>
      </div>
    </div>
  )
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
