import type { AnyRouter } from '@tanstack/react-router'

/** The signed-in app's routes, whose code a sign-in leads to. */
const SIGNED_IN = ['/_authed', '/_authed/_chat', '/_authed/_chat/', '/_authed/_chat/chat/$conversationId'] as const

/**
 * Fetches the signed-in app's code while a sign-in form waits for the person,
 * once the page is idle. The routes' code is split from the sign-in pages',
 * and was otherwise downloaded only after the password checked out - on a
 * server far away, most of the wait between pressing the button and seeing the
 * chat. Only code: no route's guard or loader runs, so nothing is asked of the
 * server without a session. Files are cached for good once fetched, so after
 * the first visit (and each release) this costs nothing.
 */
export function warmSignedInApp(router: AnyRouter): () => void {
  const load = () => {
    for (const id of SIGNED_IN) {
      const route = router.routesById[id]
      if (route) void router.loadRouteChunk(route)?.catch(() => undefined)
    }
  }
  if (typeof window.requestIdleCallback === 'function') {
    const handle = window.requestIdleCallback(load, { timeout: 2000 })
    return () => window.cancelIdleCallback(handle)
  }
  const timer = window.setTimeout(load, 300)
  return () => window.clearTimeout(timer)
}
