import { describe, expect, it } from 'vitest'

import { ARRIVAL_FADE_MS, MAX_BATCH_CHARS, coverOpacity, nextArrivals, noArrivals } from './arrival'

describe('nextArrivals', () => {
  it('takes in silently text that grew with no new text arriving, or by more than a batch fades', () => {
    const state = nextArrivals(noArrivals, 10, 0)
    expect(nextArrivals(state, 400, 50, { arrived: false })).toEqual({ shown: 400, batches: [] })
    expect(nextArrivals(state, 10 + MAX_BATCH_CHARS + 1, 50)).toEqual({ shown: 10 + MAX_BATCH_CHARS + 1, batches: [] })
  })

  it('shows what is there at first as it is', () => {
    expect(nextArrivals(noArrivals, 120, 0)).toEqual({ shown: 120, batches: [] })
  })

  it('fades in a fresh part from its first character', () => {
    expect(nextArrivals(noArrivals, 40, 0, { fresh: true }).batches).toEqual([{ start: 0, end: 40, born: 0 }])
  })

  it('makes what a render adds one batch', () => {
    const first = nextArrivals(noArrivals, 10, 0)
    const second = nextArrivals(first, 25, 100)
    expect(second).toEqual({ shown: 25, batches: [{ start: 10, end: 25, born: 100 }] })
    const third = nextArrivals(second, 30, 200)
    expect(third.batches).toEqual([
      { start: 10, end: 25, born: 100 },
      { start: 25, end: 30, born: 200 },
    ])
  })

  it('lets a batch go once it has faded in', () => {
    const state = nextArrivals(nextArrivals(noArrivals, 10, 0), 20, 0)
    expect(nextArrivals(state, 20, ARRIVAL_FADE_MS).batches).toEqual([])
  })

  it('just shows text that grew where nothing may fade', () => {
    expect(nextArrivals(nextArrivals(noArrivals, 10, 0), 20, 50, { fading: false })).toEqual({ shown: 20, batches: [] })
  })

  it('drops every batch when the text is cut short', () => {
    const state = nextArrivals(nextArrivals(noArrivals, 10, 0), 20, 50)
    expect(nextArrivals(state, 15, 60)).toEqual({ shown: 15, batches: [] })
  })
})

describe('coverOpacity', () => {
  it('goes from opaque to clear over the fade', () => {
    expect(coverOpacity(0, 0)).toBe(1)
    expect(coverOpacity(0, ARRIVAL_FADE_MS / 2)).toBeCloseTo(0.5)
    expect(coverOpacity(0, ARRIVAL_FADE_MS)).toBe(0)
    expect(coverOpacity(0, ARRIVAL_FADE_MS * 2)).toBe(0)
  })
})
