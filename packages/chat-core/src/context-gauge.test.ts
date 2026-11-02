import { describe, expect, it } from 'vitest'

import { compactTokens, contextGauge } from './context-gauge'

describe('contextGauge', () => {
  it('sets what was used against the window, warning as it fills', () => {
    expect(contextGauge(94_000, 258_000)).toEqual({ share: 94_000 / 258_000, percent: 36, level: null, used: '94k', limit: '258k' })
    expect(contextGauge(210_000, 258_000)?.level).toBe('high')
    expect(contextGauge(300_000, 258_000)).toMatchObject({ share: 1, percent: 100, level: 'full' })
  })

  it('shows nothing before an answer or for a window it does not know', () => {
    expect(contextGauge(undefined, 258_000)).toBeNull()
    expect(contextGauge(1200, null)).toBeNull()
  })

  it('names windows the way models are known by', () => {
    expect([compactTokens(800), compactTokens(32_000), compactTokens(1_200_000), compactTokens(1_000_000)]).toEqual(['800', '32k', '1.2M', '1M'])
  })
})
