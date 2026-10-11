import { describe, expect, it } from 'vitest'

import { outputSpeed } from './format'

const row = (over: Partial<Parameters<typeof outputSpeed>[0]> = {}) => ({
  status: 'ok' as const,
  estimated: false,
  outputTokens: 643,
  durationMs: 5909,
  ...over,
})

describe('outputSpeed', () => {
  it('is output tokens over the whole request', () => {
    expect(outputSpeed(row())).toBeCloseTo(108.8, 1)
  })

  it('stays possible when the reasoning was billed but not streamed', () => {
    // 7,040 tokens, the first of them arriving 0.1 s before the end of a 60 s
    // request: once 70,400 t/s.
    expect(outputSpeed(row({ outputTokens: 7040, durationMs: 60_100 }))).toBeLessThan(200)
  })

  it('is nothing for rows too small to measure, failed or estimated', () => {
    expect(outputSpeed(row({ outputTokens: 4, durationMs: 1988 }))).toBeUndefined()
    expect(outputSpeed(row({ durationMs: 120 }))).toBeUndefined()
    expect(outputSpeed(row({ status: 'error' }))).toBeUndefined()
    expect(outputSpeed(row({ estimated: true }))).toBeUndefined()
  })
})
