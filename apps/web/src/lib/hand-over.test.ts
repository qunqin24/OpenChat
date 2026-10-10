import { describe, expect, it, vi } from 'vitest'

import { refreshConfirmed, refreshThenSettle } from './hand-over'

const ok = () => Promise.resolve()
const fail = () => Promise.reject(new Error('offline'))

describe('refreshConfirmed', () => {
  it('is true when the newest page was spliced in', async () => {
    const refetchAll = vi.fn(ok)
    expect(await refreshConfirmed({ refreshNewest: ok, refetchAll })).toBe(true)
    expect(refetchAll).not.toHaveBeenCalled()
  })

  it('falls back to the whole refetch, and is true if that works', async () => {
    expect(await refreshConfirmed({ refreshNewest: fail, refetchAll: ok })).toBe(true)
  })

  it('is false when the refetch fails too - the fallback resolving is not enough', async () => {
    expect(await refreshConfirmed({ refreshNewest: fail, refetchAll: fail })).toBe(false)
  })
})

describe('refreshThenSettle', () => {
  const wait = () => Promise.resolve()

  it('settles the run once the refresh is confirmed', async () => {
    const settle = vi.fn()
    const done = await refreshThenSettle({ refresh: () => Promise.resolve(true), settle, runId: 'r1', wait, cancelled: () => false })
    expect(done).toBe(true)
    expect(settle).toHaveBeenCalledExactlyOnceWith('r1')
  })

  it('keeps the run when the refresh fails, and settles it when a later try succeeds', async () => {
    const settle = vi.fn()
    const results = [false, false, true]
    const refresh = vi.fn(() => Promise.resolve(results.shift() ?? false))
    const waits: number[] = []
    const done = await refreshThenSettle({
      refresh,
      settle,
      runId: 'r1',
      wait: (ms) => {
        // Never settled while a refresh was still failing.
        expect(settle).not.toHaveBeenCalled()
        waits.push(ms)
        return Promise.resolve()
      },
      cancelled: () => false,
      delays: [10, 20, 30],
    })
    expect(done).toBe(true)
    expect(refresh).toHaveBeenCalledTimes(3)
    expect(waits).toEqual([10, 20])
    expect(settle).toHaveBeenCalledExactlyOnceWith('r1')
  })

  it('gives up without settling when every try fails', async () => {
    const settle = vi.fn()
    const refresh = vi.fn(() => Promise.resolve(false))
    const done = await refreshThenSettle({ refresh, settle, runId: 'r1', wait, cancelled: () => false, delays: [1, 2] })
    expect(done).toBe(false)
    expect(refresh).toHaveBeenCalledTimes(3)
    expect(settle).not.toHaveBeenCalled()
  })

  it('stops trying once cancelled (the screen was left)', async () => {
    const settle = vi.fn()
    const refresh = vi.fn(() => Promise.resolve(false))
    let gone = false
    const done = await refreshThenSettle({
      refresh,
      settle,
      runId: 'r1',
      wait: () => {
        gone = true
        return Promise.resolve()
      },
      cancelled: () => gone,
    })
    expect(done).toBe(false)
    expect(refresh).toHaveBeenCalledTimes(1)
  })

  it('settles nothing when no run is held, but still reports the refresh', async () => {
    const settle = vi.fn()
    const done = await refreshThenSettle({ refresh: () => Promise.resolve(true), settle, wait, cancelled: () => false })
    expect(done).toBe(true)
    expect(settle).not.toHaveBeenCalled()
  })
})
