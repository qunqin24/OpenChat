import { useEffect, useRef, useState } from 'react'

/**
 * Whether a sticky header is pinned right now: scrolled past its own place and
 * held at the top of the transcript. CSS can say where a box sticks but not
 * whether it is stuck (scroll-state queries are Chromium-only), and a pinned
 * header is drawn differently — a background that feathers into the top bar —
 * from one sitting in the flow, where that background would fog the text
 * beside it.
 *
 * The header must be the first thing in its parent, as the reasoning toggle
 * and the activity line are: it is stuck exactly when it has moved down from
 * its parent's top edge.
 */
export function useStuck<T extends HTMLElement>(active: boolean) {
  const ref = useRef<T>(null)
  const [stuck, setStuck] = useState(false)

  useEffect(() => {
    const el = ref.current
    const scroller = el?.closest('.n-scroll')
    if (!active || !el || !el.parentElement || !scroller) return
    const parent = el.parentElement
    let frame = 0
    const check = () => {
      frame = 0
      setStuck(el.getBoundingClientRect().top - parent.getBoundingClientRect().top > 0.5)
    }
    const onScroll = () => {
      if (!frame) frame = requestAnimationFrame(check)
    }
    check()
    scroller.addEventListener('scroll', onScroll, { passive: true })
    return () => {
      scroller.removeEventListener('scroll', onScroll)
      if (frame) cancelAnimationFrame(frame)
      setStuck(false)
    }
  }, [active])

  return [ref, stuck] as const
}
