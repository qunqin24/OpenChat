// How streamed text appears in both apps: whatever one render adds is one
// batch, shown at once and faded in over ARRIVAL_FADE_MS together, the way it
// came. The web lays a cover per line over a batch (apps/web arrival-fade.ts),
// the app per laid-out line (apps/mobile components/chat/ArrivalFade.tsx);
// what counts as a batch, and how a cover fades, are these.

/** How long a batch of text takes to fade in, in ms. */
export const ARRIVAL_FADE_MS = 600

/** A batch of text that arrived at once: its place in the text, in characters, and when. */
export interface ArrivalBatch {
  start: number
  end: number
  born: number
}

/**
 * The most characters one batch fades. A bigger one - a relay's whole answer,
 * a page opened on a finished reply - is shown as it is: there is nothing
 * left to watch arrive, and covering it would measure every line of it.
 */
export const MAX_BATCH_CHARS = 4000

/** ease-in-out, as the opacity of what lies over the text. */
export const easeInOut = (t: number) => (t < 0.5 ? 2 * t * t : 1 - (-2 * t + 2) ** 2 / 2)

/** The opacity of a cover over a batch born at `born`: opaque when it arrives, clear ARRIVAL_FADE_MS later. */
export function coverOpacity(born: number, now: number): number {
  return 1 - easeInOut(Math.min(1, Math.max(0, (now - born) / ARRIVAL_FADE_MS)))
}

export interface ArrivalState {
  /** How much of the text has been shown; null before the first render. */
  shown: number | null
  batches: ArrivalBatch[]
}

export const noArrivals: ArrivalState = { shown: null, batches: [] }

/**
 * The batches after a render whose text is `length` characters long. What the
 * text holds when it first renders is shown as it is, unless `fresh` (it
 * arrived after the reader was watching), when it fades in too. Text that grew
 * adds a batch - unless `fading` is false (reduced motion, an app in the
 * background), it grew by more than MAX_BATCH_CHARS, or no new text `arrived`
 * (a diagram drawn after its source grows the rendered text on its own: the
 * web once covered its hundreds of labels as one batch and froze the page),
 * when it is just shown. Text cut short drops every batch: what is left was
 * there already. Batches past their fade are let go.
 */
export function nextArrivals(
  state: ArrivalState,
  length: number,
  now: number,
  { fresh = false, fading = true, arrived = true } = {},
): ArrivalState {
  const shown = state.shown ?? (fresh ? 0 : length)
  let batches = state.batches
  if (length > shown) {
    if (fading && arrived && length - shown <= MAX_BATCH_CHARS) batches = [...batches, { start: shown, end: length, born: now }]
  } else if (length < shown) {
    batches = []
  }
  return { shown: length, batches: batches.filter((batch) => now - batch.born < ARRIVAL_FADE_MS) }
}
