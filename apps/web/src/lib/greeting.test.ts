import { describe, expect, it } from 'vitest'

import { dayPeriod } from './greeting'

describe('dayPeriod', () => {
  it('greets by the hour, and late at night says so', () => {
    expect([4, 5, 10, 11, 12, 13, 17, 18, 21, 22, 0].map(dayPeriod)).toEqual([
      'night', 'morning', 'morning', 'noon', 'noon', 'afternoon', 'afternoon', 'evening', 'evening', 'night', 'night',
    ])
  })
})
