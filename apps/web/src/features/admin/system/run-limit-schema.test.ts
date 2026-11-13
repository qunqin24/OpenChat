import { describe, expect, it } from 'vitest'

import { limitField, MAX_RUNS } from './run-limit-schema'

const field = limitField(MAX_RUNS, {
  required: 'required',
  whole: 'whole',
  range: (max) => `0-${max}`,
})

const message = (result: ReturnType<typeof field.safeParse>) => (result.success ? undefined : result.error.issues[0]?.message)

describe('the run limit field', () => {
  it('rejects an empty field instead of reading it as 0, which is unlimited', () => {
    for (const empty of ['', '   ']) {
      const result = field.safeParse(empty)
      expect(result.success).toBe(false)
      expect(message(result)).toBe('required')
    }
  })

  it('keeps 0 as the explicit "no limit", typed or stored', () => {
    expect(field.safeParse('0')).toMatchObject({ success: true, data: 0 })
    expect(field.safeParse(0)).toMatchObject({ success: true, data: 0 })
  })

  it('reads whole numbers typed in the box', () => {
    expect(field.safeParse('25')).toMatchObject({ success: true, data: 25 })
  })

  it('rejects fractions, text and numbers out of range', () => {
    expect(message(field.safeParse('2.5'))).toBe('whole')
    expect(message(field.safeParse('abc'))).toBe('whole')
    expect(message(field.safeParse('-1'))).toBe(`0-${MAX_RUNS}`)
    expect(message(field.safeParse(String(MAX_RUNS + 1)))).toBe(`0-${MAX_RUNS}`)
  })
})
