import { useEffect, useRef, useState } from 'react'
import { type AnimateOptions, Streamdown } from 'streamdown'

import { markdownComponents, markdownLinkSafety, prepareMarkdown, useMarkdownPlugins } from './markdown-components'

/**
 * How text that streams in appears. The page gets an answer in bursts — the
 * server commits what the model wrote every 150 ms, and a slow commit, a
 * proxy or a relay that holds the stream back, or a tab coming back from the
 * background can deliver much more at once. Each character fades in, and the
 * characters of one burst are spread over time (`stagger` each, compressed so
 * the page is never more than `maxBacklogMs` behind what it holds), so a burst
 * reads as typing rather than a paragraph appearing. Per character, not per
 * word: Chinese has no spaces, and a "word" would be the whole burst.
 */
const streamingAnimation: AnimateOptions = { animation: 'fadeIn', duration: 160, sep: 'char', stagger: 12, maxBacklogMs: 400 }

/** Markdown as the chat draws it: an answer's text, the reasoning, a conversation's summary. */
export function Markdown({ text, streaming = false }: { text: string; streaming?: boolean }) {
  const plugins = useMarkdownPlugins(text)
  const ref = useRef<HTMLDivElement>(null)
  const animating = useLingering(streaming, ref)
  return (
    <div className="n-markdown" ref={ref}>
      {/* The local typography layer styles the actual Streamdown output;
          markdown-body keeps the ported table selectors working. */}
      <div className="markdown-body">
        <Streamdown className="n-prose" parseIncompleteMarkdown={animating} animated={streamingAnimation} isAnimating={animating} components={markdownComponents} plugins={plugins} linkSafety={markdownLinkSafety}>
          {prepareMarkdown(text)}
        </Streamdown>
      </div>
    </div>
  )
}

/**
 * Streaming, and for as long after as the characters already on the page are
 * still waiting their turn to appear. A burst near the end leaves a queue
 * (the reveal never goes faster than a character every 4 ms), and turning the
 * animation off with the stream would show the rest of it at once — the very
 * jump the animation is there to avoid. The element stays the same across the
 * hand-over (see the part keys in `AssistantTurn`), so this outlives the
 * stream; a remount just shows the text.
 */
function useLingering(streaming: boolean, ref: React.RefObject<HTMLElement | null>): boolean {
  const [lingering, setLingering] = useState(false)
  const [wasStreaming, setWasStreaming] = useState(streaming)
  if (streaming !== wasStreaming) {
    setWasStreaming(streaming)
    setLingering(!streaming)
  }
  useEffect(() => {
    if (!lingering) return
    const left = remainingReveal(ref.current)
    if (left <= 0) {
      setLingering(false)
      return
    }
    const timer = window.setTimeout(() => setLingering(false), left)
    return () => window.clearTimeout(timer)
  }, [lingering, ref])
  return streaming || lingering
}

/** How long until every character in el has finished fading in, in ms. */
function remainingReveal(el: HTMLElement | null): number {
  if (!el) return 0
  let left = 0
  for (const animation of el.getAnimations({ subtree: true })) {
    // Only the reveal's own (`sd-fadeIn`, the list markers' `sd-markerIn`),
    // not a spinner's that never ends.
    if (!(animation instanceof CSSAnimation) || !animation.animationName.startsWith('sd-')) continue
    const { endTime, localTime } = animation.effect?.getComputedTiming() ?? {}
    if (typeof endTime === 'number' && typeof localTime === 'number') left = Math.max(left, endTime - localTime)
  }
  return Math.min(left, 10_000)
}
