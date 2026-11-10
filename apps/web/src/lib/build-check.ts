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

/**
 * Whether an error is a piece of the app's code that could not be fetched.
 *
 * The app is built in pieces that are fetched when first needed (a route, the
 * Word and PDF views, a settings tab), each under a name that holds a hash of
 * its content. After an upgrade the server has the new pieces only, so a page
 * loaded before it asks for names that are gone (a connection that dropped
 * gives the same error). Each browser words it its own way; the last is
 * Vite's, for a stylesheet a piece needs.
 */
export function isChunkLoadError(error: unknown): boolean {
  const message = error instanceof Error ? error.message : typeof error === 'string' ? error : ''
  return /Failed to fetch dynamically imported module|error loading dynamically imported module|Importing a module script failed|Unable to preload CSS/i.test(
    message,
  )
}

let pieceListener: (() => void) | null = null
let pieceTold = false

/** Who tells the reader that a piece could not be fetched (main.tsx: a toast with a reload button). */
export function onPieceMissing(fn: () => void) {
  pieceListener = fn
}

/**
 * A piece of the app could not be fetched: the reader is told, once, and not
 * at all when they already know the server was upgraded. Said as what is
 * known - it did not load, reloading is the way out - because the same error
 * is what a dropped connection gives.
 */
export function notePieceMissing() {
  if (pieceTold || watch.told) return
  pieceTold = true
  pieceListener?.()
}

/** notePieceMissing for an error that is one (isChunkLoadError); says whether it was. */
export function noteChunkError(error: unknown): boolean {
  if (!isChunkLoadError(error)) return false
  notePieceMissing()
  return true
}
