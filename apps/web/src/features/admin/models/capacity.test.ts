import { describe, expect, it } from 'vitest'

import { capacityText, formatMoney, parseCapacity, usedRatio } from './capacity'

describe('parseCapacity', () => {
  it('reads empty as no cap', () => {
    expect(parseCapacity('')).toEqual({ ok: true, value: null })
    expect(parseCapacity('   ')).toEqual({ ok: true, value: null })
  })

  it('reads positive amounts with up to six decimals', () => {
    expect(parseCapacity('20')).toEqual({ ok: true, value: 20 })
    expect(parseCapacity(' 0.30 ')).toEqual({ ok: true, value: 0.3 })
    expect(parseCapacity('0.000001')).toEqual({ ok: true, value: 0.000001 })
    expect(parseCapacity('1000000000')).toEqual({ ok: true, value: 1_000_000_000 })
  })

  it.each(['0', '0.0', '-1', '1.2345678', '1e3', '1,5', '$5', 'abc', '.5', '5.', '1000000001'])('refuses %j', (raw) => {
    expect(parseCapacity(raw)).toEqual({ ok: false })
  })
})

describe('capacityText', () => {
  it('writes a cap without trailing zeros and nothing for no cap', () => {
    expect(capacityText(20)).toBe('20')
    expect(capacityText(0.5)).toBe('0.5')
    expect(capacityText(12.345678)).toBe('12.345678')
    expect(capacityText(null)).toBe('')
    expect(capacityText(undefined)).toBe('')
  })
})

describe('formatMoney', () => {
  it('shows cents from a cent up', () => {
    expect(formatMoney(12.3)).toBe('$12.30')
    expect(formatMoney(1234.5)).toBe('$1,234.50')
    expect(formatMoney(0.01)).toBe('$0.01')
  })

  it('shows as many decimals as a small amount needs', () => {
    expect(formatMoney(0.0003)).toBe('$0.0003')
    expect(formatMoney(0.000001)).toBe('$0.000001')
  })

  it('shows zero as zero', () => {
    expect(formatMoney(0)).toBe('$0.00')
    expect(formatMoney(0, true)).toBe('$0')
  })

  it('drops the cents of a whole amount when compact', () => {
    expect(formatMoney(20, true)).toBe('$20')
    expect(formatMoney(12.34, true)).toBe('$12.34')
    expect(formatMoney(20)).toBe('$20.00')
  })
})

describe('usedRatio', () => {
  it('is the share used, kept within 0 and 1', () => {
    expect(usedRatio(5, 20)).toBe(0.25)
    expect(usedRatio(30, 20)).toBe(1)
    expect(usedRatio(0, 20)).toBe(0)
    expect(usedRatio(5, 0)).toBe(0)
  })
})
