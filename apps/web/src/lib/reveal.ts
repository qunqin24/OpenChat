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
 * caught up within a fraction of a second.
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
 * its pace. The text arrives in batches - every 150 ms from the server, every
 * half second or so from some relays - and a reveal that shows each batch as
 * soon as it can runs dry before the next: a few characters at a sprint, then a
 * stop. Kept a quarter of a second behind, it always has the next characters to
 * show, and a slow model reads as even typing rather than stop and go.
 */
export const REVEAL_LAG_S = 0.25
/** How quickly the reveal closes on that lag, in seconds; a burst is shown within about this. */
export const REVEAL_CATCH_UP_S = 0.4
/** The slowest the reveal goes while anything is left, in characters a second. */
export const REVEAL_MIN_CPS = 6
/** Over how long the arrival rate is averaged, in seconds. */
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
}

/** The state of a reveal that has shown all of `length` characters. */
export const revealDone = (length: number, rate = 0): RevealState => ({ shown: length, speed: 0, rate, seen: length })

/**
 * One step of the reveal: `dt` ms after `state`, with `target` characters held.
 *
 * The speed aims at the pace the text arrives at, plus whatever closes the gap
 * to REVEAL_LAG_S behind it within REVEAL_CATCH_UP_S: a steady stream of any
 * speed is shown at its own pace, evenly, a burst is caught up quickly, and
 * once the stream ends (the rate falls away) what is left is shown within the
 * catch-up time. The speed eases towards that aim, so batches blur into one
 * pace.
 */
export function revealStep(state: RevealState, target: number, dt: number): RevealState {
  const seconds = dt / 1000
  const arrived = Math.max(0, target - state.seen)
  const rate = seconds > 0 ? state.rate + (arrived / seconds - state.rate) * Math.min(1, seconds / RATE_WINDOW_S) : state.rate
  if (state.shown >= target) return revealDone(target, rate)
  const backlog = target - state.shown
  const aim = Math.max(REVEAL_MIN_CPS, rate + (backlog - rate * REVEAL_LAG_S) / REVEAL_CATCH_UP_S)
  const speed = state.speed + (aim - state.speed) * Math.min(1, dt / SPEED_EASE_MS)
  return { shown: Math.min(target, state.shown + speed * seconds), speed, rate, seen: target }
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
