import { useIsFetching, useIsMutating } from '@tanstack/react-query'
import { useRouterState } from '@tanstack/react-router'
import { useEffect, useState } from 'react'

/** Work shorter than this never shows a bar — a flash is worse than nothing. */
const SHOW_AFTER_MS = 300

/** How long the finished bar stays at 100% before fading out. */
const SETTLE_MS = 220

/**
 * One progress bar for the whole app: route transitions, queries and
 * mutations all feed it.
 *
 * The width is faked on purpose. Neither a route load nor a fetch reports
 * progress, so the bar eases toward 90% and waits there, then jumps to 100%
 * when the work actually finishes. A bar that reached 100% on its own would
 * claim to be done while the screen was still empty.
 */
export function GlobalProgress() {
  const isRouting = useRouterState({ select: (state) => state.status === 'pending' })
  const fetching = useIsFetching()
  const mutating = useIsMutating()

  const busy = isRouting || fetching > 0 || mutating > 0

  const [visible, setVisible] = useState(false)
  const [progress, setProgress] = useState(0)

  useEffect(() => {
    if (!busy) return

    // Nothing is shown until the work has lasted long enough to be worth
    // mentioning; most cached navigations never get here.
    const show = window.setTimeout(() => {
      setVisible(true)
      setProgress(8)
    }, SHOW_AFTER_MS)

    return () => window.clearTimeout(show)
  }, [busy])

  // Creep toward 90% while the work is outstanding, in ever smaller steps.
  useEffect(() => {
    if (!visible || !busy) return

    const timer = window.setInterval(() => {
      setProgress((current) => (current >= 90 ? current : current + (90 - current) * 0.18))
    }, 220)

    return () => window.clearInterval(timer)
  }, [visible, busy])

  useEffect(() => {
    if (busy || !visible) return

    // Deferred a frame rather than set synchronously: a setState during the
    // effect would re-render before the browser has painted the current width,
    // and the bar would jump to 100% without the fill ever being seen moving.
    const frame = requestAnimationFrame(() => setProgress(100))
    const hide = window.setTimeout(() => {
      setVisible(false)
      setProgress(0)
    }, SETTLE_MS)

    return () => {
      cancelAnimationFrame(frame)
      window.clearTimeout(hide)
    }
  }, [busy, visible])

  if (!visible) return null

  return (
    <div className="n-progress" role="progressbar" aria-hidden="true">
      <div className="n-progress-bar" style={{ width: `${progress}%` }} />
    </div>
  )
}

export default GlobalProgress
