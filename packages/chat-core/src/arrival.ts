// How streamed text appears in both apps: whatever one render adds is one
// batch, laid out at once and swept in top to bottom at the speed text has been
// coming, a piece at a time, each piece fading in over ARRIVAL_FADE_MS. The web lays a cover per line over a piece (apps/web
// arrival-fade.ts), the app per laid-out line (apps/mobile
// components/chat/ArrivalFade.tsx); what counts as a batch, how it is swept in
// and how a cover fades, are these.
//
// The sweep is for fast models: the server publishes what a model wrote every
// 50ms, so at a few thousand characters a second each batch is a line or more,
// and shown at once the answer came out in blocks. Sweeping each batch over the
// time its text takes at that speed - about the gap before the next - makes the
// text flow as it comes, a gap behind.
//
// It is also for a stream that stops: a provider now and then goes quiet for
// seconds part-way through an answer and then sends the rest at once. Shown at
// once, a screenful of text jumped out of nowhere and read as the page having
// hung. So a burst flows in at the speed text was coming before it, however
// many renders it takes to arrive, and is all in by MAX_CATCH_UP_MS after the
// last of them: the end is never later than that.

/** How long a piece of text takes to fade in, in ms. */
export const ARRIVAL_FADE_MS = 200

/** The latest the text that has arrived is all in, in ms after it arrived, however much came at once. */
export const MAX_CATCH_UP_MS = 2500

/** How long a batch takes to sweep in before there is any telling how fast text comes, in ms. */
const FIRST_SWEEP_MS = 50

/**
 * Batches further apart than this, in ms, are a stream that paused, not its
 * speed: the server publishes every 50ms while a model writes.
 */
const RATE_WINDOW_MS = 250

/** How far apart the pieces of a batch start to appear, in ms: about a frame. */
const STEP_MS = 16

/** How much of a new measure of the speed goes into the running estimate of it. */
const RATE_WEIGHT = 0.3

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
  /** When text last arrived, and the running estimate of how fast it comes, in characters a ms. */
  at: number | null
  rate: number | null
}

export const noArrivals: ArrivalState = { shown: null, batches: [], due: 0, at: null, rate: null }

/**
 * Where a stream's sweeps have got to, apart from where its text is: texts
 * drawn from one stream - the app's blocks of one answer - share it, so a new
 * block follows on from the one before instead of appearing ahead of its end.
 */
export type ArrivalPacing = Pick<ArrivalState, 'due' | 'at' | 'rate'>

export const noPacing: ArrivalPacing = { due: 0, at: null, rate: null }

export const pacingOf = ({ due, at, rate }: ArrivalState): ArrivalPacing => ({ due, at, rate })

/**
 * The pieces of a batch of the characters [start, end) arriving at `now`, and
 * when its sweep ends: it starts once the batch before is swept in (`due`) and
 * takes `span`, but ends no later than MAX_CATCH_UP_MS from now - so batches
 * that come close together (held up on the way, then all at once) follow on
 * as one burst, and the last is in on time.
 */
function sweep(start: number, end: number, now: number, due: number, span: number): { pieces: ArrivalBatch[]; until: number } {
  const from = Math.max(now, due)
  const until = Math.max(from, Math.min(from + span, now + MAX_CATCH_UP_MS))
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
  let { batches, due, at, rate } = state
  if (length > shown && arrived) {
    const chars = length - shown
    // The speed is taken from batches that came as a stream does; one after a
    // pause, or one of a burst split over renders, measures nothing.
    const since = at === null ? null : now - at
    if (since !== null && since >= STEP_MS && since <= RATE_WINDOW_MS) {
      const measured = chars / since
      rate = rate === null ? measured : rate + RATE_WEIGHT * (measured - rate)
    }
    at = now
    if (fading && chars <= MAX_BATCH_CHARS) {
      const swept = sweep(shown, length, now, due, rate === null ? FIRST_SWEEP_MS : chars / rate)
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
  return { shown: length, batches: batches.filter((batch) => now - batch.born < ARRIVAL_FADE_MS), due, at, rate }
}

/**
 * How long an answer may stop growing part-way through, in ms, before the page
 * says it is still being written: a provider going quiet for seconds otherwise
 * looks like the page having hung.
 */
export const STILL_WRITING_MS = 2000

/**
 * Whether a reply being written should say it still is: the run goes on, the
 * last thing in it is text, and nothing else on screen says the model is at
 * work (the thinking line, a tool's activity). How long it has been quiet is
 * the caller's to time, by `STILL_WRITING_MS`.
 */
export function mayStallWriting(lastPart: { type: string; text?: string } | undefined, otherwiseBusy: boolean): boolean {
  return !otherwiseBusy && lastPart?.type === 'text' && Boolean(lastPart.text?.trim())
}
