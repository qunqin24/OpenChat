import { describe, expect, it } from 'vitest'

import { bannedUntil } from './ban'

describe('bannedUntil', () => {
  it('reads when the ban ends', () => {
    expect(bannedUntil({ code: 'guest_banned', message: 'x', details: { until: '2026-10-18T10:00:00Z' } })).toBe('2026-10-18T10:00:00Z')
  })

  it('reads a permanent ban as null', () => {
    expect(bannedUntil({ code: 'guest_banned', details: { until: null } })).toBeNull()
    expect(bannedUntil({ code: 'guest_banned' })).toBeNull()
    expect(bannedUntil({ code: 'guest_banned', details: { until: '' } })).toBeNull()
  })

  it('is undefined for anything else', () => {
    expect(bannedUntil({ code: 'guest_ip_limit' })).toBeUndefined()
    expect(bannedUntil(null)).toBeUndefined()
    expect(bannedUntil('guest_banned')).toBeUndefined()
    expect(bannedUntil(undefined)).toBeUndefined()
  })
})
