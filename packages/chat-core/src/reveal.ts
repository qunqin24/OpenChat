/**
 * The pace at which streamed text is shown.
 *
 * An answer reaches the page in bursts - the server commits what the model
 * wrote every 150 ms, and a slow commit, a proxy or relay that holds the stream
 * back, or a tab coming back from the background can deliver much more at once.
 * Shown as it arrives, a burst is a paragraph jumping onto the screen. So the
 * page shows a growing prefix of what it holds instead, a little more each
 * frame, at the pace the text has been arriving at and a little behind it: a
 * steady stream reads as steady typing whatever the model's speed, a burst is
 * caught up over about a second, and once the stream ends what is left is
 * shown within a fraction of one.
 *
 * Only the length shown changes; the text stays one string, rendered as
 * Markdown like any other. (An earlier version faded every character in as its
 * own element: thousands of nodes and animations in a long reasoning, and the
 * page grew slower the longer the model thought.)
 *
 * Pure, so the pacing is tested without a clock or a DOM.
 */

/**
 * How much the reveal keeps in hand while the stream is steady, in seconds of
 * its pace, at the least. The text arrives in batches - every 150 ms from the
 * server, every half second or more from some relays - and a reveal that shows
 * each batch as soon as it can runs dry before the next: a few characters at a
 * sprint, then a stop. Kept a batch behind (this, or the time between batches
 * when that is longer), it always has the next characters to show, and a slow
 * model reads as even typing rather than stop and go. The stream's end is
 * known (`streaming`), so what is kept in hand costs nothing there.
 */
export const REVEAL_LAG_S = 0.25
/**
 * How quickly the reveal closes on that lag, in seconds, at the least: a burst
 * - a slow commit, a relay that held the stream, the first batch of a part -
 * is shown over about this, as typing; over about the time between batches
 * when that is longer, so a relay's batch is still being typed when the next
 * one comes. (At 0.4 s a paragraph went by too fast to see as anything but
 * appearing.)
 */
export const REVEAL_CATCH_UP_S = 1
/**
 * The longest the time between batches counts for, in seconds: a model that
 * pauses to think mid-answer is not a relay, and its next words should not be
 * held back for as long as it was quiet.
 */
const REVEAL_BATCH_MAX_S = 2
/** The time between batches the reveal assumes before it has seen any, in seconds (the server's commits). */
const REVEAL_BATCH_S = 0.15
/**
 * How long what is left takes once the stream has ended, in seconds, at the
 * most. The reader has been told the answer is done: the end is shown at once
 * rather than typed out for another second, but still as the end of the
 * typing rather than a jump.
 */
export const REVEAL_FINISH_S = 0.3
/**
 * How long a part that arrives whole while the reader watches takes to show,
 * in seconds - an answer short enough that the model finished it between two
 * commits, a channel that does not stream: it comes out as typing, quickly,
 * rather than appearing in one piece.
 */
export const REVEAL_WHOLE_S = 0.6
/** The pace the last of a burst is shown at, in characters a second. */
export const REVEAL_CLOSE_CPS = 30
/** After how long without anything arriving the stream counts as quiet, in seconds. */
const REVEAL_QUIET_S = 1
/** The slowest the reveal goes while anything is left, in characters a second. */
export const REVEAL_MIN_CPS = 6
/** Over how long the arrival rate is averaged, in seconds, at the least (two batches when they are further apart). */
const RATE_WINDOW_S = 1
/** How quickly the speed follows its aim, in ms (it eases rather than jumps). */
const SPEED_EASE_MS = 120

export interface RevealState {
  /** Characters (UTF-16 units) shown; fractional, a character shows once whole. */
  shown: number
  /** The current speed, in characters a second. */
  speed: number
  /** How fast the text has been arriving, in characters a second (averaged). */
  rate: number
  /** The length of the text at the last step, to tell what has arrived since. */
  seen: number
  /** How long since anything arrived, in seconds. */
  quiet: number
  /** The time between batches, in seconds (averaged). */
  batch: number
  /**
   * Once the stream has ended: the even pace what is left is finished at, in
   * characters a second, so it is done within REVEAL_FINISH_S. Null while it
   * streams.
   */
  finish: number | null
}

/**
 * Where the reveal of a part that mounts while streaming starts: this many
 * characters from its end at most. Enough for the first batch of a new part to
 * be typed rather than appear; little enough that a page opened part-way
 * through a long answer only retypes its last line or two. A part the reader
 * watched begin starts from nothing (`fresh` in MarkdownText.tsx).
 */
export const REVEAL_FRESH_CHARS = 160
export const revealFrom = (length: number) => Math.max(0, length - REVEAL_FRESH_CHARS)

/** The state of a reveal that has shown all of `length` characters. */
export const revealDone = (length: number, rate = 0, batch = REVEAL_BATCH_S): RevealState => ({
  shown: length,
  speed: 0,
  rate,
  seen: length,
  quiet: 0,
  batch,
  finish: null,
})

/**
 * The state of a part that arrived whole, all `length` characters of it, while
 * the reader watched: shown from nothing, evenly, within REVEAL_WHOLE_S.
 */
export const revealWhole = (length: number): RevealState => ({
  ...revealDone(length),
  shown: 0,
  finish: length / REVEAL_WHOLE_S,
})

/**
 * One step of the reveal: `dt` ms after `state`, with `target` characters held,
 * and `streaming` whether more may come.
 *
 * While it streams, the speed aims at the pace the text arrives at, plus
 * whatever closes the gap to a batch behind it within the catch-up time: a
 * steady stream of any speed is shown at its own pace, evenly, a burst is
 * caught up over about a second (or the time between a relay's batches), and
 * the speed eases towards that aim, so batches blur into one pace. Once the
 * stream has ended, what is left is finished at an even pace within
 * REVEAL_FINISH_S - never slower than it was going - rather than closing on
 * it ever more slowly.
 */
export function revealStep(state: RevealState, target: number, dt: number, streaming = true): RevealState {
  const seconds = dt / 1000
  const arrived = Math.max(0, target - state.seen)
  // The time between batches, from the time since the one before.
  const batch = arrived > 0 ? state.batch + (Math.min(state.quiet + seconds, REVEAL_BATCH_MAX_S) - state.batch) / 2 : state.batch
  const window = Math.max(RATE_WINDOW_S, 2 * batch)
  const rate = seconds > 0 ? state.rate + (arrived / seconds - state.rate) * Math.min(1, seconds / window) : state.rate
  const quiet = arrived > 0 ? 0 : state.quiet + seconds
  if (state.shown >= target) return { ...revealDone(target, rate, batch), quiet }
  const backlog = target - state.shown
  if (!streaming) {
    // Set when the stream ends, and again if anything arrives after.
    const finish = state.finish === null || arrived > 0 ? Math.max(state.finish ?? 0, state.speed, backlog / REVEAL_FINISH_S) : state.finish
    return { shown: Math.min(target, state.shown + finish * seconds), speed: finish, rate, seen: target, quiet, batch, finish }
  }
  // Once nothing has arrived for a while (the model paused, or a part came in
  // one piece), what is left is finished at a typing pace: closing the gap in
  // proportion to its size would show the last characters ever more slowly.
  const floor = quiet >= REVEAL_QUIET_S ? Math.max(REVEAL_MIN_CPS, Math.min(REVEAL_CLOSE_CPS, backlog * 10)) : REVEAL_MIN_CPS
  const lag = Math.max(REVEAL_LAG_S, batch)
  const aim = Math.max(floor, rate + (backlog - rate * lag) / Math.max(REVEAL_CATCH_UP_S, batch))
  const speed = state.speed + (aim - state.speed) * Math.min(1, dt / SPEED_EASE_MS)
  return { shown: Math.min(target, state.shown + speed * seconds), speed, rate, seen: target, quiet, batch, finish: null }
}

/**
 * The first `shown` units of `text` (whole ones), without splitting a surrogate pair (an
 * emoji, a rare CJK character): a half shown would draw as a replacement glyph
 * for a frame.
 */
export function revealed(text: string, shown: number): string {
  const count = Math.floor(shown)
  if (count >= text.length) return text
  if (count <= 0) return ''
  const code = text.charCodeAt(count - 1)
  const cut = code >= 0xd800 && code <= 0xdbff ? count - 1 : count
  return text.slice(0, cut)
}
