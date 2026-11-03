// How streamed text appears in both apps: whatever one render adds is one
// batch, laid out at once and swept in top to bottom over about as long as
// batches have been coming apart, a piece at a time, each piece fading in over
// ARRIVAL_FADE_MS. The web lays a cover per line over a piece (apps/web
// arrival-fade.ts), the app per laid-out line (apps/mobile
// components/chat/ArrivalFade.tsx); what counts as a batch, how it is swept in
// and how a cover fades, are these.
//
// The sweep is for fast models: the server publishes what a model wrote every
// 50ms, so at a few thousand characters a second each batch is a line or more,
// and shown at once the answer came out in blocks. Sweeping each batch over the
// gap before the next makes the text flow at the speed it comes, a gap behind.
// A sweep never runs past one gap from its batch's arrival - spreading a burst
// over as long as text took to come made it crawl in for up to a second, with
// blank lines below it - so nothing queues up and the end is never late.

/** How long a piece of text takes to fade in, in ms. */
export const ARRIVAL_FADE_MS = 200

/** The longest a batch takes to sweep in, in ms, however long the gap before it. */
export const MAX_SWEEP_MS = 200

/** How long a batch takes to sweep in before there is any telling how far apart batches come, in ms. */
const FIRST_SWEEP_MS = 50

/** How far apart the pieces of a batch start to appear, in ms: about a frame. */
const STEP_MS = 16

/** How much of a new gap between batches goes into the running estimate of it. */
const GAP_WEIGHT = 0.3

/** A piece of text that appears at once: its place in the text, in characters, and when it starts to. */
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

/** The opacity of a cover over a piece born at `born`: opaque until it is due, clear ARRIVAL_FADE_MS later. */
export function coverOpacity(born: number, now: number): number {
  return 1 - easeInOut(Math.min(1, Math.max(0, (now - born) / ARRIVAL_FADE_MS)))
}

export interface ArrivalState {
  /** How much of the text has been shown; null before the first render. */
  shown: number | null
  batches: ArrivalBatch[]
  /** When the sweep of the last batch so far ends. */
  due: number
  /** When text last arrived, and the running estimate of the gap between arrivals, in ms. */
  at: number | null
  gap: number | null
}

export const noArrivals: ArrivalState = { shown: null, batches: [], due: 0, at: null, gap: null }

/**
 * Where a stream's sweeps have got to, apart from where its text is: texts
 * drawn from one stream - the app's blocks of one answer - share it, so a new
 * block follows on from the one before instead of appearing ahead of its end.
 */
export type ArrivalPacing = Pick<ArrivalState, 'due' | 'at' | 'gap'>

export const noPacing: ArrivalPacing = { due: 0, at: null, gap: null }

export const pacingOf = ({ due, at, gap }: ArrivalState): ArrivalPacing => ({ due, at, gap })

/**
 * The pieces of a batch of the characters [start, end) arriving at `now`, and
 * when its sweep ends: it starts once the batch before is swept in (`due`) and
 * is done `sweep` from now - so batches that come close together (held up on
 * the way, then all at once) share the time instead of queueing.
 */
function sweep(start: number, end: number, now: number, due: number, span: number): { pieces: ArrivalBatch[]; until: number } {
  const from = Math.max(now, due)
  const until = Math.max(from, now + span)
  const count = Math.max(1, Math.min(end - start, Math.round((until - from) / STEP_MS)))
  const pieces: ArrivalBatch[] = []
  for (let i = 0; i < count; i++) {
    pieces.push({
      start: start + Math.floor(((end - start) * i) / count),
      end: start + Math.floor(((end - start) * (i + 1)) / count),
      born: from + ((until - from) * i) / count,
    })
  }
  return { pieces, until }
}

/**
 * The pieces after a render whose text is `length` characters long. What the
 * text holds when it first renders is shown as it is, unless `fresh` (it
 * arrived after the reader was watching), when it is revealed too. Text that
 * grew adds a batch, swept in - unless `fading` is false (reduced motion, an app
 * in the background) or it grew by more than MAX_BATCH_CHARS, when everything
 * is just shown, or no new text `arrived` (a diagram drawn after its source
 * grows the rendered text on its own: the web once covered its hundreds of
 * labels as one batch and froze the page), when the growth is taken in
 * silently. Text cut short drops every piece: what is left was there already.
 * Pieces past their fade are let go.
 */
export function nextArrivals(
  state: ArrivalState,
  length: number,
  now: number,
  { fresh = false, fading = true, arrived = true } = {},
): ArrivalState {
  const shown = state.shown ?? (fresh ? 0 : length)
  let { batches, due, at, gap } = state
  if (length > shown && arrived) {
    if (at !== null) {
      const since = Math.min(MAX_SWEEP_MS, now - at)
      gap = gap === null ? since : gap + GAP_WEIGHT * (since - gap)
    }
    at = now
    if (fading && length - shown <= MAX_BATCH_CHARS) {
      const swept = sweep(shown, length, now, due, Math.min(MAX_SWEEP_MS, gap ?? FIRST_SWEEP_MS))
      batches = [...batches, ...swept.pieces]
      due = swept.until
    } else {
      batches = []
      due = 0
    }
  } else if (length < shown) {
    batches = []
    due = 0
  }
  return { shown: length, batches: batches.filter((batch) => now - batch.born < ARRIVAL_FADE_MS), due, at, gap }
}
