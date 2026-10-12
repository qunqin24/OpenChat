/**
 * Telling a page that the server it talks to has been upgraded under it.
 *
 * Every response carries the build of the front end the server embeds
 * (`X-OpenChat-Build`, a hash of its index.html). The first value a page sees
 * is the build it was loaded from; a different one later means the server now
 * serves another front end - the image was upgraded and the server restarted
 * (the event stream reconnecting is usually the first request to show it) -
 * and this page's code is out of date. The reader is told once, and reloads
 * when it suits them: Open WebUI reloads the page by itself, which would throw
 * away a half-written message.
 *
 * No header (development, where Vite serves the page) means nothing to compare.
 */

export const BUILD_HEADER = 'X-OpenChat-Build'

export interface BuildWatch {
  /** The build this page was loaded from, as the first response named it. */
  loaded: string | null
  /** Whether the reader has been told already. */
  told: boolean
}

/** The watch after seeing `build`, and whether to tell the reader now. */
export function seeBuild(watch: BuildWatch, build: string | null): { watch: BuildWatch; tell: boolean } {
  if (!build) return { watch, tell: false }
  if (watch.loaded === null) return { watch: { ...watch, loaded: build }, tell: false }
  if (build === watch.loaded || watch.told) return { watch, tell: false }
  return { watch: { ...watch, told: true }, tell: true }
}

let watch: BuildWatch = { loaded: null, told: false }
let listener: (() => void) | null = null

/** Who tells the reader (main.tsx: a toast with a reload button). */
export function onBuildChange(fn: () => void) {
  listener = fn
}

/** Looks at a response's build; every request the app makes passes through here. */
export function noteBuild(response: Response) {
  const next = seeBuild(watch, response.headers.get(BUILD_HEADER))
  watch = next.watch
  if (next.tell) listener?.()
}
