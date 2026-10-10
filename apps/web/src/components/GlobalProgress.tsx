import { useIsFetching, useIsMutating } from '@tanstack/react-query'
import { useRouterState } from '@tanstack/react-router'
import { useEffect, useState } from 'react'

/** Work shorter than this never shows a bar — a flash is worse than nothing. */
const SHOW_AFTER_MS = 300

/** How long the finished bar stays at 100% before fading out. */
const SETTLE_MS = 220

/**
 * Whether the admin panel is where the router is going, or already is. The
 * router's `location` is the address being navigated to from the moment the
 * navigation starts (`resolvedLocation` is the last one that finished loading),
 * so it answers for a pending navigation and a settled one alike.
 */
function useTargetsAdmin(): boolean {
  return useRouterState({
    select: (state) => state.location.pathname === '/admin' || state.location.pathname.startsWith('/admin/'),
  })
}

/**
 * The bar of the admin panel, and only of the panel (mounted at the root, which
 * is why it is a component of its own rather than a line in routes/admin.tsx):
 * the admin layout is not on screen yet on the very first navigation into it
 * from the chat — it renders once that navigation has finished — so a bar mounted
 * there starts too late to show the wait it is for. Here it is mounted as soon as
 * the route being navigated to is an admin one, and not for a navigation to
 * anywhere else, so the chat, which shows its own progress in place, never
 * gets one.
 */
export function AdminProgress() {
  return useTargetsAdmin() ? <GlobalProgress /> : null
}

/**
 * The admin panel's progress bar (mounted by AdminProgress, not by the chat):
 * route transitions, first loads and actions without an indicator of their own
 * feed it.
 *
 * The width is faked on purpose. Neither a route load nor a fetch reports
 * progress, so the bar eases toward 90% and waits there, then jumps to 100%
 * when the work actually finishes. A bar that reached 100% on its own would
 * claim to be done while the screen was still empty.
 */
export function GlobalProgress() {
  const isRouting = useRouterState({ select: (state) => state.status === 'pending' })
  // Only loads with nothing to show yet: a refetch behind data already on the
  // screen (after an answer, on reconnect) is the page keeping itself current,
  // not something the reader is waiting for.
  const fetching = useIsFetching({ predicate: (query) => query.state.data === undefined })
  // Actions that show their own progress where they happen — a reply waiting
  // in the transcript, a row saying it is being named — opt out with
  // `meta: { silent: true }`, or the bar would announce them twice.
  const mutating = useIsMutating({ predicate: (mutation) => !mutation.options.meta?.silent })

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
      <div className="n-progress-bar" data-motion="essential" style={{ width: `${progress}%` }} />
    </div>
  )
}

export default GlobalProgress
