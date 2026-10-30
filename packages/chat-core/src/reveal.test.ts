import { describe, expect, it } from 'vitest'

import {
  REVEAL_FINISH_S,
  REVEAL_FRESH_CHARS,
  REVEAL_MIN_CPS,
  REVEAL_WHOLE_S,
  revealDone,
  revealed,
  revealFrom,
  revealStep,
  revealWhole,
  type RevealState,
} from './reveal'

const FRAME = 1000 / 60
const start: RevealState = revealDone(0)

/** Runs the reveal for `ms`, with `arrive(t)` the characters held at time t. */
function run(state: RevealState, arrive: (t: number) => number, ms: number, from = 0, streaming = true) {
  const steps: { t: number; shown: number; held: number }[] = []
  for (let t = from; t < from + ms; t += FRAME) {
    const held = arrive(t)
    state = revealStep(state, held, FRAME, streaming)
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

  it('types a burst out over about a second rather than showing it at once', () => {
    const { steps } = run(start, () => 500, 2500)
    expect(steps[0]!.shown).toBeLessThan(20)
    // Visibly typing a third of a second in, most of it shown within a second.
    expect(steps.find((s) => s.t >= 300)!.shown).toBeLessThan(250)
    expect(steps.find((s) => s.t >= 1000)!.shown).toBeGreaterThan(350)
    expect(steps.at(-1)!.shown).toBe(500)
  })

  it('types the first batch of a part that starts mid-stream', () => {
    // A short answer whose first batch is a whole sentence: 60 characters.
    const from = revealFrom(60)
    expect(from).toBe(0)
    const { steps } = run({ ...revealDone(60), shown: from }, () => 60, 2500)
    // Typed out at reading pace - not there at once, all there within two seconds,
    // and the last of it not crawling.
    expect(steps.find((s) => s.t >= 200)!.shown).toBeLessThan(20)
    expect(steps.find((s) => s.t >= 2000)!.shown).toBe(60)
    const tenToGo = steps.find((s) => s.shown >= 50)!.t
    expect(steps.find((s) => s.shown === 60)!.t - tenToGo).toBeLessThan(500)
    // A long part opened part-way through retypes only its end.
    expect(revealFrom(5000)).toBe(5000 - REVEAL_FRESH_CHARS)
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
    // The lag kept in hand plus a batch: about half a second's worth at most.
    expect(worst).toBeLessThan(110)
  })

  it('finishes what is left soon after the stream ends', () => {
    const streaming = run(start, (t) => Math.min(300, batches(25, 150)(t)), 1800)
    // The stream stopped at 300 characters; the rest shows within a second.
    const { state } = run(streaming.state, () => 300, 1000, 1800)
    expect(state.shown).toBe(300)
  })

  it('finishes within a fraction of a second once told the stream is over, typing to the end', () => {
    // A relay that held the end back: a steady stream, then 1500 characters
    // in the commit that ends it.
    const before = run(start, batches(9, 150), 5000)
    const ended = run(before.state, () => before.steps.at(-1)!.held + 1500, 1000, 5000, false)
    const done = ended.steps.find((s) => s.shown === s.held)!
    expect(done.t - 5000).toBeLessThanOrEqual(REVEAL_FINISH_S * 1000 + FRAME)
    // Still typed, not jumped: the first frame shows only a little of it.
    expect(ended.steps[0]!.shown - before.steps.at(-1)!.shown).toBeLessThan(150)
    // And never slower towards the end, the way closing on a gap would.
    const moved = ended.steps.slice(1, ended.steps.indexOf(done)).map((s, i) => s.shown - ended.steps[i]!.shown)
    expect(Math.min(...moved.slice(0, -1))).toBeGreaterThanOrEqual(Math.max(...moved) - 1)
  })

  it('keeps typing between a slow relay’s batches', () => {
    // 300 characters every second and a half: once it has seen a few, a
    // batch is typed over the time to the next rather than in a second and
    // then a stop.
    const { steps } = run(start, batches(300, 1500), 12_000)
    expect(Math.max(...gaps(steps.filter((s) => s.t > 4500)))).toBeLessThan(100)
  })

  it('types out a part that arrived whole, quickly', () => {
    const { steps } = run(revealWhole(600), () => 600, 1000, 0, false)
    expect(steps[0]!.shown).toBeLessThan(40)
    expect(steps.find((s) => s.t >= (REVEAL_WHOLE_S * 1000) / 2)!.shown).toBeLessThan(400)
    expect(steps.find((s) => s.shown === 600)!.t).toBeLessThanOrEqual(REVEAL_WHOLE_S * 1000 + FRAME)
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
