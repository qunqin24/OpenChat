import { describe, expect, it } from 'vitest'

import { REVEAL_MIN_CPS, revealed, revealStep, type RevealState } from './reveal'

const FRAME = 1000 / 60

function run(state: RevealState, target: number, ms: number): RevealState {
  for (let t = 0; t < ms; t += FRAME) state = revealStep(state, target, FRAME)
  return state
}

describe('revealStep', () => {
  it('stops where the text ends', () => {
    expect(revealStep({ shown: 10, speed: 50 }, 10, FRAME)).toEqual({ shown: 10, speed: 0 })
    expect(revealStep({ shown: 12, speed: 50 }, 10, FRAME)).toEqual({ shown: 10, speed: 0 })
  })

  it('always moves while there is something left', () => {
    expect(revealStep({ shown: 0, speed: 0 }, 1, FRAME).shown).toBe(1)
  })

  it('spreads a burst over a fraction of a second rather than showing it at once', () => {
    const start: RevealState = { shown: 0, speed: 0 }
    const first = revealStep(start, 500, FRAME)
    expect(first.shown).toBeLessThan(50)
    // Most of it within half a second, all of it within a second and a bit.
    expect(run(start, 500, 500).shown).toBeGreaterThan(300)
    expect(run(start, 500, 1500).shown).toBe(500)
  })

  it('keeps pace with a steady stream without falling behind', () => {
    // 25 characters every 150 ms, for three seconds.
    let state: RevealState = { shown: 0, speed: 0 }
    let target = 0
    let worst = 0
    for (let t = 0; t < 3000; t += FRAME) {
      if (Math.floor(t / 150) !== Math.floor((t - FRAME) / 150)) target += 25
      state = revealStep(state, target, FRAME)
      worst = Math.max(worst, target - state.shown)
    }
    // Never more than about half a second's worth behind.
    expect(worst).toBeLessThan(90)
  })

  it('never goes slower than the floor while behind', () => {
    const state = revealStep({ shown: 0, speed: 0 }, 2, 1000)
    expect(state.shown).toBe(2)
    expect(REVEAL_MIN_CPS).toBeGreaterThan(0)
  })
})

describe('revealed', () => {
  it('is a prefix, and the whole text once caught up', () => {
    expect(revealed('你好世界', 2)).toBe('你好')
    expect(revealed('你好', 9)).toBe('你好')
    expect(revealed('abc', 0)).toBe('')
  })

  it('does not cut a surrogate pair in half', () => {
    const text = 'a😀b'
    expect(revealed(text, 2)).toBe('a')
    expect(revealed(text, 3)).toBe('a😀')
  })
})
