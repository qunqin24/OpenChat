import { afterEach, describe, expect, it, vi } from 'vitest'

import { idbStorage } from './idb'
import { openGuestStorage } from './open-storage'

// IndexedDB that answers an open as the script says: 'ok', 'error', 'blocked',
// or never (a browser that does not answer at all).
type Outcome = 'ok' | 'error' | 'blocked' | 'never'

function fakeIndexedDB(script: Outcome[]) {
  const opened: string[] = []
  const db = { objectStoreNames: { contains: () => true } }
  const open = (name: string) => {
    opened.push(name)
    const outcome = script.shift() ?? 'ok'
    const request: {
      result: unknown
      error: unknown
      onsuccess?: () => void
      onerror?: () => void
      onblocked?: () => void
      onupgradeneeded?: () => void
    } = { result: db, error: new Error('indexedDB: denied') }
    queueMicrotask(() => {
      if (outcome === 'ok') request.onsuccess?.()
      else if (outcome === 'error') request.onerror?.()
      else if (outcome === 'blocked') request.onblocked?.()
    })
    return request
  }
  return { open, opened }
}

afterEach(() => {
  vi.unstubAllGlobals()
  vi.useRealTimers()
})

describe('openGuestStorage', () => {
  it('keeps conversations in IndexedDB when it opens', async () => {
    vi.stubGlobal('indexedDB', fakeIndexedDB(['ok']))
    const opened = await openGuestStorage()
    expect(opened.kept).toBe(true)
  })

  it('falls back to memory, and says nothing is kept, when it cannot be opened', async () => {
    vi.stubGlobal('indexedDB', fakeIndexedDB(['error']))
    const opened = await openGuestStorage()
    expect(opened.kept).toBe(false)
    // And it works: a conversation can be written and read back, for this visit.
    await opened.storage.putPersonalization({ about: 'hi' } as never)
    expect(await opened.storage.personalization()).toEqual({ about: 'hi' })
  })

  it('falls back when the open is blocked', async () => {
    vi.stubGlobal('indexedDB', fakeIndexedDB(['blocked']))
    expect((await openGuestStorage()).kept).toBe(false)
  })

  it('falls back when the browser never answers', async () => {
    vi.useFakeTimers()
    vi.stubGlobal('indexedDB', fakeIndexedDB(['never']))
    const pending = openGuestStorage()
    await vi.advanceTimersByTimeAsync(5000)
    expect((await pending).kept).toBe(false)
  })

  it('falls back when the browser has no IndexedDB at all', async () => {
    vi.stubGlobal('indexedDB', undefined)
    expect((await openGuestStorage()).kept).toBe(false)
  })
})

describe('idbStorage', () => {
  it('does not remember a failed open for the life of the page', async () => {
    const fake = fakeIndexedDB(['error', 'ok'])
    vi.stubGlobal('indexedDB', fake)
    const storage = idbStorage()
    await expect(storage.probe()).rejects.toThrow()
    // The cause may have passed; the next call tries again.
    await expect(storage.probe()).resolves.toBeUndefined()
    expect(fake.opened).toHaveLength(2)
  })
})
