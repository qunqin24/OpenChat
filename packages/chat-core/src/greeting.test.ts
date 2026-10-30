import { describe, expect, it } from 'vitest'

import { dayPeriod, greetingLines, pickGreeting } from './greeting'

describe('dayPeriod', () => {
  it('greets by the hour, and late at night says so', () => {
    expect([4, 5, 10, 11, 12, 13, 17, 18, 21, 22, 0].map(dayPeriod)).toEqual([
      'night', 'morning', 'morning', 'noon', 'noon', 'afternoon', 'afternoon', 'evening', 'evening', 'night', 'night',
    ])
  })
})

// 2026-10-26 is a Monday; months are zero-based.
const at = (day: number, hour: number) => new Date(2026, 9, 26 + day, hour)

describe('greetingLines', () => {
  it('keeps to the lines without the name when there is none', () => {
    expect(greetingLines(at(1, 15), false).every((line) => !line.named)).toBe(true)
    expect(greetingLines(at(1, 15), true).some((line) => line.named)).toBe(true)
  })

  it('adds a weekday line only at its time', () => {
    const keys = (day: number, hour: number) => greetingLines(at(day, hour), true).map((line) => line.key)
    expect(keys(0, 8)).toContain('chat.welcome.lines.monday.ease')
    expect(keys(0, 15)).not.toContain('chat.welcome.lines.monday.ease')
    expect(keys(4, 15)).toContain('chat.welcome.lines.friday.almost')
    expect(keys(4, 20)).toContain('chat.welcome.lines.friday-night.unwind')
    expect(keys(4, 20)).not.toContain('chat.welcome.lines.friday.almost')
    expect(keys(5, 10)).toContain('chat.welcome.lines.weekend.slow')
    // The small hours of Saturday are still the night.
    expect(keys(5, 2)).not.toContain('chat.welcome.lines.weekend.slow')
  })
})

describe('greetingLines on a holiday', () => {
  it('says only the day\'s own lines, from midnight on', () => {
    const keys = (hour: number, named: boolean) => greetingLines(new Date(2026, 11, 25, hour), named).map((line) => line.key)
    expect(keys(1, true)).toEqual(['chat.welcome.holidays.12-25.hello', 'chat.welcome.holidays.12-25.gift'])
    expect(keys(15, false)).toEqual(['chat.welcome.holidays.12-25.gift'])
  })

  it('every holiday has a line without the name', () => {
    for (const month of [0, 1, 3, 4, 5, 9, 11]) {
      for (let day = 1; day <= 31; day++) {
        expect(greetingLines(new Date(2026, month, day, 12), false).length).toBeGreaterThan(0)
      }
    }
  })
})

describe('pickGreeting', () => {
  it('never repeats the last line when there is another', () => {
    const lines = greetingLines(at(1, 15), true)
    for (const last of lines) {
      for (const r of [0, 0.5, 0.999]) expect(pickGreeting(lines, last.key, () => r).key).not.toBe(last.key)
    }
  })

  it('repeats a line that is the only one', () => {
    const only = pickGreeting(greetingLines(at(1, 15), true), null)
    expect(pickGreeting([only], only.key).key).toBe(only.key)
  })
})
