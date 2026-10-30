import type { Task } from '@openchat/api-client'
import { describe, expect, it } from 'vitest'

import { emptyForm, formOf, inPast, scheduleOf } from './tasks'

const now = new Date(2026, 9, 27, 14, 20) // a Tuesday, local time

describe('emptyForm', () => {
  it('starts daily at the next full hour', () => {
    const form = emptyForm(now)
    expect(form).toMatchObject({ kind: 'daily', date: '2026-10-27', time: '15:00', weekdays: [2], monthDay: 27 })
  })
})

describe('scheduleOf', () => {
  const base = emptyForm(now)

  it('writes a one-off as an instant', () => {
    const schedule = scheduleOf({ ...base, kind: 'once', date: '2026-10-28', time: '09:00' }, 'Asia/Shanghai')
    expect(schedule).toEqual({ kind: 'once', at: new Date(2026, 9, 28, 9, 0).toISOString(), timeZone: 'Asia/Shanghai' })
  })

  it('keeps only what each kind needs', () => {
    expect(scheduleOf({ ...base, kind: 'daily', time: '08:30' }, 'UTC')).toEqual({ kind: 'daily', time: '08:30', timeZone: 'UTC' })
    expect(scheduleOf({ ...base, kind: 'weekly', time: '08:30', weekdays: [5, 1, 5] }, 'UTC')).toEqual({
      kind: 'weekly', time: '08:30', weekdays: [1, 5], timeZone: 'UTC',
    })
    expect(scheduleOf({ ...base, kind: 'monthly', time: '08:30', monthDay: 31 }, 'UTC')).toEqual({
      kind: 'monthly', time: '08:30', monthDay: 31, timeZone: 'UTC',
    })
  })

  it('is null for what is not a schedule yet', () => {
    expect(scheduleOf({ ...base, kind: 'weekly', weekdays: [] })).toBeNull()
    expect(scheduleOf({ ...base, time: '' })).toBeNull()
    expect(scheduleOf({ ...base, kind: 'once', date: '' })).toBeNull()
  })
})

describe('formOf', () => {
  const task = (schedule: Task['schedule']): Task => ({
    id: 't', title: 'News', prompt: 'p', schedule, status: 'active', conversationId: 'c', createdAt: now.toISOString(),
  })

  it('reads a one-off back in the browser clock', () => {
    const form = formOf(task({ kind: 'once', at: new Date(2026, 10, 3, 7, 5).toISOString(), timeZone: 'UTC' }), now)
    expect(form).toMatchObject({ kind: 'once', date: '2026-11-03', time: '07:05', title: 'News' })
  })

  it('keeps a repeating schedule as it is', () => {
    expect(formOf(task({ kind: 'weekly', time: '18:00', weekdays: [1, 3], timeZone: 'UTC' }), now)).toMatchObject({
      kind: 'weekly', time: '18:00', weekdays: [1, 3],
    })
  })
})

describe('inPast', () => {
  it('is only ever true of a one-off', () => {
    expect(inPast({ ...emptyForm(now), kind: 'once', date: '2026-10-27', time: '14:00' }, now)).toBe(true)
    expect(inPast({ ...emptyForm(now), kind: 'once', date: '2026-10-27', time: '15:00' }, now)).toBe(false)
    expect(inPast({ ...emptyForm(now), kind: 'daily', time: '01:00' }, now)).toBe(false)
  })
})
