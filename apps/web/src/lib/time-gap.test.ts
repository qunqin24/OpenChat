import { describe, expect, it } from 'vitest'

import { gapLabel, timeGaps } from './time-gap'
import type { Turn } from './transcript'

const user = (key: string, createdAt: string) => ({ kind: 'user', key, message: { createdAt } }) as unknown as Turn
const reply = (key: string, createdAt: string) =>
  ({ kind: 'assistant', key, responses: [{ createdAt }] }) as unknown as Turn

describe('timeGaps', () => {
  it('marks the person\'s messages that come an hour or more after the turn before', () => {
    const gaps = timeGaps([
      user('u1', '2026-10-28T01:00:00Z'),
      reply('a1', '2026-10-28T01:01:00Z'),
      user('u2', '2026-10-28T01:40:00Z'),
      reply('a2', '2026-10-28T01:41:00Z'),
      user('u3', '2026-10-28T02:41:00Z'),
    ])
    expect([...gaps.keys()]).toEqual(['u3'])
    expect(gaps.get('u3')).toBe(Date.parse('2026-10-28T02:41:00Z'))
  })
})

describe('gapLabel', () => {
  const words = { today: '今天', yesterday: '昨天' }
  const now = new Date(2026, 9, 28, 15, 0).getTime()
  it('says today, yesterday, the day, or the day and year', () => {
    expect(gapLabel(new Date(2026, 9, 28, 10, 44).getTime(), now, 'zh-CN', words)).toBe('今天 10:44')
    expect(gapLabel(new Date(2026, 9, 27, 21, 5).getTime(), now, 'zh-CN', words)).toBe('昨天 21:05')
    expect(gapLabel(new Date(2026, 8, 3, 9, 0).getTime(), now, 'zh-CN', words)).toBe('9月3日 09:00')
    expect(gapLabel(new Date(2025, 8, 3, 9, 0).getTime(), now, 'zh-CN', words)).toBe('2025年9月3日 09:00')
  })
})
