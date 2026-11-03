import { describe, expect, it } from 'vitest'

import { glyphColor } from './plugin-icon'

describe('glyphColor', () => {
  it('is white on dark brands and dark on light ones', () => {
    expect(glyphColor('#181717')).toBe('#fff')
    expect(glyphColor('#F7DF1E')).toBe('#111')
    expect(glyphColor('')).toBe('#fff')
  })
})
