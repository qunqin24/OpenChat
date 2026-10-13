import { describe, expect, it } from 'vitest'

import { REVEAL_MIN_CPS, revealDone, revealed, revealStep, type RevealState } from './reveal'

const FRAME = 1000 / 60
const start: RevealState = revealDone(0)

/** Runs the reveal for `ms`, with `arrive(t)` the characters held at time t. */
function run(state: RevealState, arrive: (t: number) => number, ms: number, from = 0) {
  const steps: { t: number; shown: number; held: number }[] = []
  for (let t = from; t < from + ms; t += FRAME) {
    const held = arrive(t)
    state = revealStep(state, held, FRAME)
    steps.push({ t, shown: Math.floor(state.shown), held })
  }
  return { state, steps }
}

/** Arrives in batches of `size` every `every` ms, from t = 0. */
const batches = (size: number, every: number) => (t: number) => (Math.floor(t / every) + 1) * size

/** The gaps between the moments a new character shows, after the first second. */
function gaps(steps: { t: number; shown: number }[]) {
  const out: number[] = []
  let last = -1
  let at = 0
  for (const { t, shown } of steps) {
    if (shown === last) continue
    if (t > 1000 && last >= 0) out.push(t - at)
    last = shown
    at = t
  }
  return out
}

describe('revealStep', () => {
  it('stops where the text ends', () => {
    expect(revealStep({ ...start, shown: 10, seen: 10 }, 10, FRAME).shown).toBe(10)
    expect(revealStep({ ...start, shown: 12, seen: 12 }, 10, FRAME).shown).toBe(10)
  })

  it('never goes slower than the floor while something is left', () => {
    const { state } = run(start, () => 2, 1000)
    expect(state.shown).toBe(2)
    expect(REVEAL_MIN_CPS).toBeGreaterThan(0)
  })

  it('spreads a burst over a fraction of a second rather than showing it at once', () => {
    const { steps } = run(start, () => 500, 1500)
    expect(steps[0]!.shown).toBeLessThan(50)
    expect(steps.find((s) => s.t >= 500)!.shown).toBeGreaterThan(250)
    expect(steps.at(-1)!.shown).toBe(500)
  })

  it('shows a slow model at an even pace, not in sprints between batches', () => {
    // About 8 characters a second, arriving 150 ms at a time (the server's commits).
    const { steps } = run(start, batches(1.2, 150), 6000)
    const between = gaps(steps)
    const mean = between.reduce((a, b) => a + b, 0) / between.length
    // A character every ~125 ms; never a stop of more than a batch and a half.
    expect(mean).toBeGreaterThan(100)
    expect(Math.max(...between)).toBeLessThan(225)
  })

  it('evens out a relay that delivers every half second', () => {
    const { steps } = run(start, batches(10, 500), 8000)
    // 20 characters a second: without the lag kept in hand this sprints for
    // 100 ms and then stops for 400.
    expect(Math.max(...gaps(steps))).toBeLessThan(150)
  })

  it('keeps a steady stream close behind', () => {
    // 25 characters every 150 ms, for three seconds.
    const { steps } = run(start, batches(25, 150), 3000)
    const worst = Math.max(...steps.filter((s) => s.t > 1000).map((s) => s.held - s.shown))
    // About the lag kept in hand plus a batch: well under half a second's worth.
    expect(worst).toBeLessThan(90)
  })

  it('finishes what is left soon after the stream ends', () => {
    const streaming = run(start, (t) => Math.min(300, batches(25, 150)(t)), 1800)
    // The stream stopped at 300 characters; the rest shows within a second.
    const { state } = run(streaming.state, () => 300, 1000, 1800)
    expect(state.shown).toBe(300)
  })
})

describe('revealed', () => {
  it('is a prefix of whole characters, and the whole text once caught up', () => {
    expect(revealed('你好世界', 2)).toBe('你好')
    expect(revealed('你好世界', 2.9)).toBe('你好')
    expect(revealed('你好', 9)).toBe('你好')
    expect(revealed('abc', 0)).toBe('')
  })

  it('does not cut a surrogate pair in half', () => {
    const text = 'a😀b'
    expect(revealed(text, 2)).toBe('a')
    expect(revealed(text, 3)).toBe('a😀')
  })
})
