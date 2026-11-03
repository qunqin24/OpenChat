import { describe, expect, it } from 'vitest'

import { ARRIVAL_FADE_MS, MAX_BATCH_CHARS, MAX_SWEEP_MS, coverOpacity, nextArrivals, noArrivals, type ArrivalState } from './arrival'

/** The text a stream of renders adds, `chars` at a time every `every` ms from 0. */
function stream(renders: number, chars: number, every: number): ArrivalState {
  let state = nextArrivals(noArrivals, 0, 0)
  for (let i = 1; i <= renders; i++) state = nextArrivals(state, i * chars, i * every)
  return state
}

/** When the characters of a batch are all under way: the last piece's start. */
const lastBorn = (state: ArrivalState) => Math.max(...state.batches.map((b) => b.born))

describe('nextArrivals', () => {
  it('takes in silently text that grew with no new text arriving, and just shows a batch too big to fade', () => {
    const state = nextArrivals(noArrivals, 10, 0)
    expect(nextArrivals(state, 400, 50, { arrived: false })).toMatchObject({ shown: 400, batches: [] })
    expect(nextArrivals(state, 10 + MAX_BATCH_CHARS + 1, 50)).toMatchObject({ shown: 10 + MAX_BATCH_CHARS + 1, batches: [] })
  })

  it('shows what is there at first as it is', () => {
    expect(nextArrivals(noArrivals, 120, 0)).toMatchObject({ shown: 120, batches: [] })
  })

  it('reveals a fresh part from its first character', () => {
    const { batches } = nextArrivals(noArrivals, 40, 0, { fresh: true })
    expect(batches.at(0)).toMatchObject({ start: 0, born: 0 })
    expect(batches.at(-1)?.end).toBe(40)
  })

  it('cuts a batch into pieces that cover it end to end, one after another', () => {
    const { batches } = nextArrivals(nextArrivals(noArrivals, 10, 0), 1210, 100)
    expect(batches.length).toBeGreaterThan(1)
    expect(batches.at(0)).toMatchObject({ start: 10, born: 100 })
    expect(batches.at(-1)?.end).toBe(1210)
    batches.slice(1).forEach((piece, i) => {
      expect(piece.start).toBe(batches[i]?.end)
      expect(piece.born).toBeGreaterThan(batches[i]?.born ?? Infinity)
    })
  })

  it('sweeps a batch in over about the gap batches come apart, however big', () => {
    for (const size of [40, 1200]) {
      const state = stream(20, size, 50)
      const arrived = 20 * 50
      expect(lastBorn(state)).toBeGreaterThan(arrived)
      expect(lastBorn(state)).toBeLessThan(arrived + 50)
    }
  })

  it('keeps up with a steady stream, each batch following on from the one before', () => {
    const state = stream(40, 150, 50)
    expect(lastBorn(state)).toBeLessThan(40 * 50 + 50)
    expect(state.batches.find((b) => b.start === 39 * 150)?.born).toBe(40 * 50)
  })

  it('sweeps a batch after a long pause in no more than MAX_SWEEP_MS', () => {
    let state = stream(20, 150, 50)
    state = nextArrivals(state, (state.shown ?? 0) + 1500, 20 * 50 + 5000)
    expect(lastBorn(state) - (20 * 50 + 5000)).toBeLessThan(MAX_SWEEP_MS)
  })

  it('never queues batches that come close together', () => {
    let state = stream(10, 150, 50)
    for (let i = 1; i <= 10; i++) state = nextArrivals(state, (state.shown ?? 0) + 300, 1000 + i)
    // Queued, ten sweeps of the gap estimate would run to about 2000.
    expect(lastBorn(state)).toBeLessThanOrEqual(1010 + MAX_SWEEP_MS)
    for (let i = 1; i < state.batches.length; i++) expect(state.batches[i]!.born).toBeGreaterThanOrEqual(state.batches[i - 1]!.born)
  })

  it('lets a piece go once it has faded in', () => {
    const state = nextArrivals(nextArrivals(noArrivals, 10, 0), 20, 0)
    expect(nextArrivals(state, 20, MAX_SWEEP_MS + ARRIVAL_FADE_MS).batches).toEqual([])
  })

  it('just shows text that grew where nothing may fade, and what was still to come', () => {
    const state = nextArrivals(nextArrivals(noArrivals, 0, 0), 1200, 0)
    expect(nextArrivals(state, 1210, 50, { fading: false })).toMatchObject({ shown: 1210, batches: [], due: 0 })
  })

  it('drops every piece when the text is cut short', () => {
    const state = nextArrivals(nextArrivals(noArrivals, 10, 0), 20, 50)
    expect(nextArrivals(state, 15, 60)).toMatchObject({ shown: 15, batches: [], due: 0 })
  })
})

describe('coverOpacity', () => {
  it('stays opaque until the piece is due, then goes clear over the fade', () => {
    expect(coverOpacity(100, 0)).toBe(1)
    expect(coverOpacity(0, 0)).toBe(1)
    expect(coverOpacity(0, ARRIVAL_FADE_MS / 2)).toBeCloseTo(0.5)
    expect(coverOpacity(0, ARRIVAL_FADE_MS)).toBe(0)
    expect(coverOpacity(0, ARRIVAL_FADE_MS * 2)).toBe(0)
  })
})
