import { describe, expect, it } from 'vitest'

import { guestLimitsSchema, MAX_HOURLY, MAX_RUNS } from './guest-limits-schema'

const schema = guestLimitsSchema({
  required: 'required',
  whole: 'whole',
  range: (max) => `0-${max}`,
})

const parse = (guestHourlyLimit: unknown, guestDailyLimit: unknown = 0, guestMaxConcurrentRuns: unknown = 2) =>
  schema.safeParse({ guestHourlyLimit, guestDailyLimit, guestMaxConcurrentRuns })

const message = (result: ReturnType<typeof parse>) => (result.success ? undefined : result.error.issues[0]?.message)

describe('the guest limit fields', () => {
  it('rejects an empty field instead of reading it as 0, which is unlimited', () => {
    for (const empty of ['', '   ']) {
      const result = parse(empty)
      expect(result.success).toBe(false)
      expect(message(result)).toBe('required')
    }
  })

  it('keeps 0 as the explicit "no limit", typed or stored', () => {
    expect(parse('0')).toMatchObject({ success: true, data: { guestHourlyLimit: 0 } })
    expect(parse(0)).toMatchObject({ success: true, data: { guestHourlyLimit: 0 } })
  })

  it('reads whole numbers typed in the box', () => {
    expect(parse('25', '1000')).toMatchObject({ success: true, data: { guestHourlyLimit: 25, guestDailyLimit: 1000 } })
  })

  it('rejects fractions, text and numbers out of range', () => {
    expect(message(parse('2.5'))).toBe('whole')
    expect(message(parse('abc'))).toBe('whole')
    expect(message(parse('-1'))).toBe(`0-${MAX_HOURLY}`)
    expect(message(parse(String(MAX_HOURLY + 1)))).toBe(`0-${MAX_HOURLY}`)
  })

  it('holds the concurrent-runs field to the same rules', () => {
    expect(message(parse(0, 0, ''))).toBe('required')
    expect(parse(0, 0, '0')).toMatchObject({ success: true, data: { guestMaxConcurrentRuns: 0 } })
    expect(message(parse(0, 0, String(MAX_RUNS + 1)))).toBe(`0-${MAX_RUNS}`)
  })
})
