/**
 * The pace at which streamed text is shown.
 *
 * An answer reaches the page in bursts - the server commits what the model
 * wrote every 150 ms, and a slow commit, a proxy or relay that holds the stream
 * back, or a tab coming back from the background can deliver much more at once.
 * Shown as it arrives, a burst is a paragraph jumping onto the screen. So the
 * page shows a growing prefix of what it holds instead, a little more each
 * frame, at a speed that follows how far behind it is: a steady stream reads as
 * steady typing, a burst is caught up within a fraction of a second.
 *
 * Only the length shown changes; the text stays one string, rendered as
 * Markdown like any other. (An earlier version faded every character in as its
 * own element: thousands of nodes and animations in a long reasoning, and the
 * page grew slower the longer the model thought.)
 *
 * Pure, so the pacing is tested without a clock or a DOM.
 */

/** How far behind the shown text may fall, roughly: the backlog is shown within this. */
export const REVEAL_CATCH_UP_MS = 400
/** The slowest the reveal goes while anything is left, in characters a second. */
export const REVEAL_MIN_CPS = 40
/** How quickly the speed follows the backlog, in ms (it eases rather than jumps). */
const SPEED_EASE_MS = 120

export interface RevealState {
  /** Characters (UTF-16 units) shown. */
  shown: number
  /** The current speed, in characters a second. */
  speed: number
}

/**
 * One step of the reveal: `dt` ms after `state`, with `target` characters held.
 * The speed aims at clearing the backlog in REVEAL_CATCH_UP_MS, never below
 * REVEAL_MIN_CPS, and eases towards that aim, so batches arriving every 150 ms
 * blur into one even pace instead of a rush at the start of each.
 */
export function revealStep(state: RevealState, target: number, dt: number): RevealState {
  if (state.shown >= target) return { shown: target, speed: 0 }
  const backlog = target - state.shown
  const aim = Math.max(REVEAL_MIN_CPS, (backlog * 1000) / REVEAL_CATCH_UP_MS)
  const ease = Math.min(1, dt / SPEED_EASE_MS)
  const speed = state.speed + (aim - state.speed) * ease
  const advance = Math.max(1, Math.round((speed * dt) / 1000))
  return { shown: Math.min(target, state.shown + advance), speed }
}

/**
 * The first `count` units of `text`, without splitting a surrogate pair (an
 * emoji, a rare CJK character): a half shown would draw as a replacement glyph
 * for a frame.
 */
export function revealed(text: string, count: number): string {
  if (count >= text.length) return text
  if (count <= 0) return ''
  const code = text.charCodeAt(count - 1)
  const cut = code >= 0xd800 && code <= 0xdbff ? count - 1 : count
  return text.slice(0, cut)
}
