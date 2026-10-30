import { useEffect, useLayoutEffect, useRef, useState } from 'react'
import { AccessibilityInfo, AppState } from 'react-native'

import {
  revealDone,
  revealed,
  revealFrom,
  revealStep,
  revealWhole,
  type RevealState,
} from '@openchat/chat-core/reveal'

/**
 * The least time between two renders of a reveal, in ms: about 30 a second.
 * Each render parses the text shown as Markdown; every frame made a long
 * reasoning cost far more, for no step a reader could tell apart.
 */
const REVEAL_FRAME_MS = 32

let reduceMotion = false
void AccessibilityInfo.isReduceMotionEnabled().then((on) => (reduceMotion = on))
AccessibilityInfo.addEventListener('reduceMotionChanged', (on) => (reduceMotion = on))

/**
 * The part of `text` to show, revealed at the pace of chat-core/reveal.ts -
 * the web's `useReveal` (MarkdownText.tsx), ported: a prefix that grows,
 * never an element per character. What the element holds when it mounts is
 * shown at once (its last lines typed while it streams); a part the reader
 * watched begin (`fresh`) types from nothing. Text that is not a continuation
 * is shown as it is. Reduced motion, and an app in the background, get the
 * text as it is.
 */
export function useReveal(text: string, streaming: boolean, fresh = false): string {
  const [shown, setShown] = useState(() => (fresh ? 0 : streaming ? revealFrom(text.length) : text.length))
  const [held, setHeld] = useState(text)
  if (held !== text) {
    setHeld(text)
    if (!text.startsWith(revealed(held, shown))) setShown(text.length)
  }
  const target = useRef(text)
  const more = useRef(streaming)
  const state = useRef<RevealState>(fresh && !streaming ? revealWhole(text.length) : { ...revealDone(text.length), shown })
  // Whether the frame loop is going. It stops when it has caught up; text that
  // arrives in the same render as that last step leaves `behind` true all
  // along, so nothing would start it again - the reveal stood still mid-answer.
  const looping = useRef(false)
  const [kick, setKick] = useState(0)
  useLayoutEffect(() => {
    target.current = text
    more.current = streaming
    state.current.shown = Math.min(Math.max(state.current.shown, shown), text.length)
    if (!looping.current && shown < text.length) setKick((k) => k + 1)
  }, [text, shown, streaming])
  const behind = shown < text.length
  useEffect(() => {
    if (!behind) {
      state.current = revealDone(target.current.length, state.current.rate, state.current.batch)
      return
    }
    const away = () => reduceMotion || AppState.currentState !== 'active'
    if (away()) {
      state.current = revealDone(target.current.length)
      setShown(target.current.length)
      return
    }
    let frame = 0
    looping.current = true
    let last = performance.now()
    let drawn = last
    // The time is read here, not taken from the frame: React Native's frame
    // timestamp is on another clock than performance.now() once the app has
    // been in the background, and the reveal then never thought a frame had
    // passed - it stopped where it was, mid-answer.
    const tick = () => {
      const now = performance.now()
      const next = revealStep(state.current, target.current.length, now - last, more.current)
      last = now
      state.current = next
      const done = next.shown >= target.current.length
      if (done || now - drawn >= REVEAL_FRAME_MS || away()) {
        drawn = now
        setShown(away() ? target.current.length : Math.floor(next.shown))
      }
      if (!done && !away()) frame = requestAnimationFrame(tick)
      else looping.current = false
    }
    frame = requestAnimationFrame(tick)
    return () => {
      cancelAnimationFrame(frame)
      looping.current = false
    }
  }, [behind, kick])

  return revealed(text, shown)
}
