import type { Task, TaskSchedule, TaskScheduleKind } from '@openchat/api-client'
import type { TFunction } from 'i18next'

/**
 * Scheduled tasks (`/tasks`): the form the page edits a schedule with, and how
 * one is put in words. A form's times are the browser's wall clock, and a
 * schedule is saved in the browser's time zone; the server runs it in that
 * zone however the person travels.
 */

export interface TaskForm {
  title: string
  prompt: string
  kind: TaskScheduleKind
  /** YYYY-MM-DD, for a one-off. */
  date: string
  /** HH:MM. */
  time: string
  /** 0 = Sunday. */
  weekdays: number[]
  monthDay: number
}

export const taskKinds: TaskScheduleKind[] = ['once', 'daily', 'weekly', 'monthly']

/** Monday first, as a week reads in both languages the page speaks. */
export const weekdayOrder = [1, 2, 3, 4, 5, 6, 0]

export function browserTimeZone(): string {
  return Intl.DateTimeFormat().resolvedOptions().timeZone || 'UTC'
}

const pad = (n: number) => String(n).padStart(2, '0')
const dateOf = (d: Date) => `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`
const timeOf = (d: Date) => `${pad(d.getHours())}:${pad(d.getMinutes())}`

/** A new task's form: daily at the next full hour, or what a suggestion fills in. */
export function emptyForm(now = new Date(), over: Partial<TaskForm> = {}): TaskForm {
  const next = new Date(now)
  next.setHours(now.getHours() + 1, 0, 0, 0)
  return {
    title: '',
    prompt: '',
    kind: 'daily',
    date: dateOf(next),
    time: timeOf(next),
    weekdays: [now.getDay()],
    monthDay: now.getDate(),
    ...over,
  }
}

/** The form for a task being edited. A one-off's time shows in the browser's clock. */
export function formOf(task: Task, now = new Date()): TaskForm {
  const s = task.schedule
  const base = emptyForm(now, { title: task.title, prompt: task.prompt, kind: s.kind })
  if (s.kind === 'once' && s.at) {
    const at = new Date(s.at)
    return { ...base, date: dateOf(at), time: timeOf(at) }
  }
  return {
    ...base,
    time: s.time ?? base.time,
    weekdays: s.weekdays?.length ? s.weekdays : base.weekdays,
    monthDay: s.monthDay ?? base.monthDay,
  }
}

/** What the form asks for, as the contract's schedule; null while it is not one. */
export function scheduleOf(form: TaskForm, timeZone = browserTimeZone()): TaskSchedule | null {
  if (!/^\d{2}:\d{2}$/.test(form.time)) return null
  if (form.kind === 'once') {
    const at = new Date(`${form.date}T${form.time}`)
    if (!/^\d{4}-\d{2}-\d{2}$/.test(form.date) || Number.isNaN(at.getTime())) return null
    return { kind: 'once', at: at.toISOString(), timeZone }
  }
  const schedule: TaskSchedule = { kind: form.kind, time: form.time, timeZone }
  if (form.kind === 'weekly') {
    if (form.weekdays.length === 0) return null
    schedule.weekdays = [...new Set(form.weekdays)].sort((a, b) => a - b)
  }
  if (form.kind === 'monthly') {
    if (!(form.monthDay >= 1 && form.monthDay <= 31)) return null
    schedule.monthDay = form.monthDay
  }
  return schedule
}

/** A one-off in the past cannot be saved; said before the server says it. */
export function inPast(form: TaskForm, now = new Date()): boolean {
  if (form.kind !== 'once') return false
  const at = new Date(`${form.date}T${form.time}`)
  return !Number.isNaN(at.getTime()) && at.getTime() <= now.getTime()
}

type T = TFunction<'common'>

/** A weekday's short name in the locale: 周一 / Mon. */
export function weekdayName(day: number, locale?: string): string {
  // 2026-10-25 was a Sunday.
  return new Intl.DateTimeFormat(locale, { weekday: 'short' }).format(new Date(2026, 9, 25 + day))
}

/** 每天 08:30 / 每周一、周五 08:30 / 每月 15 日 08:30 / 10月28日 09:00. */
export function describeSchedule(schedule: TaskSchedule, t: T, locale?: string): string {
  const zone = schedule.timeZone !== browserTimeZone() ? ` (${schedule.timeZone})` : ''
  switch (schedule.kind) {
    case 'once': {
      const at = schedule.at ? new Date(schedule.at) : null
      if (!at) return t('tasks.schedule.once-none')
      return t('tasks.schedule.once', { when: formatWhen(at, locale) })
    }
    case 'daily':
      return t('tasks.schedule.daily', { time: schedule.time }) + zone
    case 'weekly': {
      const days = weekdayOrder
        .filter((d) => schedule.weekdays?.includes(d))
        .map((d) => weekdayName(d, locale))
        .join(t('tasks.schedule.separator'))
      return t('tasks.schedule.weekly', { days, time: schedule.time }) + zone
    }
    case 'monthly':
      return t('tasks.schedule.monthly', { day: schedule.monthDay, time: schedule.time }) + zone
  }
}

/** 10月28日 周二 09:00 — with the year only when it is not this one. */
export function formatWhen(at: Date, locale?: string, now = new Date()): string {
  return new Intl.DateTimeFormat(locale, {
    ...(at.getFullYear() !== now.getFullYear() ? { year: 'numeric' } : {}),
    month: 'short',
    day: 'numeric',
    weekday: 'short',
    hour: '2-digit',
    minute: '2-digit',
    hourCycle: 'h23',
  }).format(at)
}

/** Ideas for an empty page, each filling the form in (the strings are under `tasks.ideas`). */
export type TaskIdeaKey = 'news' | 'weekly-review' | 'learn' | 'meals' | 'deals' | 'monthly'

export const taskIdeas: { key: TaskIdeaKey; emoji: string; form: Partial<TaskForm> }[] = [
  { key: 'news', emoji: '📰', form: { kind: 'daily', time: '08:00' } },
  { key: 'weekly-review', emoji: '🗓️', form: { kind: 'weekly', time: '18:00', weekdays: [5] } },
  { key: 'learn', emoji: '📚', form: { kind: 'daily', time: '21:00' } },
  { key: 'meals', emoji: '🥗', form: { kind: 'weekly', time: '10:00', weekdays: [0] } },
  { key: 'deals', emoji: '✈️', form: { kind: 'weekly', time: '09:00', weekdays: [1] } },
  { key: 'monthly', emoji: '💰', form: { kind: 'monthly', time: '09:00', monthDay: 1 } },
]
