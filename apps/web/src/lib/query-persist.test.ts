import { QueryClient } from '@tanstack/react-query'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { queryKeys } from './query-keys'

// The module keeps who the cache belongs to in module state, so each test gets
// a fresh copy of it, and a fake localStorage and window to go with.
type Persist = typeof import('./query-persist')
const KEY = 'openchat.cache.v1'

let store: Map<string, string>
let pagehide: (() => void) | undefined

beforeEach(() => {
  vi.resetModules()
  vi.useFakeTimers()
  store = new Map()
  pagehide = undefined
  vi.stubGlobal('localStorage', {
    getItem: (k: string) => store.get(k) ?? null,
    setItem: (k: string, v: string) => void store.set(k, v),
    removeItem: (k: string) => void store.delete(k),
  })
  vi.stubGlobal('window', {
    addEventListener: (type: string, fn: () => void) => {
      if (type === 'pagehide') pagehide = fn
    },
  })
})

afterEach(() => {
  vi.useRealTimers()
  vi.unstubAllGlobals()
})

const load = (): Promise<Persist> => import('./query-persist')
const client = () => new QueryClient({ defaultOptions: { queries: { retry: false } } })

interface Saved {
  owner: string
  state: { queries: { queryKey: unknown[]; state: { data: unknown } }[] }
}
const stored = () => JSON.parse(store.get(KEY) ?? 'null') as Saved | null
const savedKeys = () => (stored()?.state.queries ?? []).map((q) => JSON.stringify(q.queryKey))
const pages = (...items: unknown[]) => ({ pages: items, pageParams: items.map(() => undefined) })

describe('persistQueryCache', () => {
  it('saves the list, models and recent transcripts a moment after they change', async () => {
    const m = await load()
    const c = client()
    m.adoptQueryCache(c, 'user-1')
    m.persistQueryCache(c)

    c.setQueryData(queryKeys.conversations.list(), pages({ items: [1] }))
    c.setQueryData(queryKeys.models.list(), [{ id: 'm' }])
    c.setQueryData(queryKeys.messages.list('c1'), pages({ items: [] }))
    c.setQueryData(queryKeys.conversations.detail('c1'), { id: 'c1' })
    c.setQueryData(queryKeys.memory.list(), ['not persisted'])

    expect(stored()).toBeNull() // debounced
    vi.advanceTimersByTime(1000)

    expect(stored()?.owner).toBe('user-1')
    expect(savedKeys().sort()).toEqual(
      [
        queryKeys.conversations.list(),
        queryKeys.models.list(),
        queryKeys.messages.list('c1'),
        queryKeys.conversations.detail('c1'),
      ]
        .map((k) => JSON.stringify(k))
        .sort(),
    )
  })

  it('keeps temporary chats out of storage', async () => {
    const m = await load()
    const c = client()
    m.adoptQueryCache(c, 'user-1')
    m.persistQueryCache(c)

    c.setQueryData(queryKeys.conversations.detail('temp'), { id: 'temp', temporary: true })
    c.setQueryData(queryKeys.messages.list('temp'), pages({ items: [] }))
    c.setQueryData(queryKeys.conversations.detail('keep'), { id: 'keep' })
    c.setQueryData(queryKeys.messages.list('keep'), pages({ items: [] }))
    vi.advanceTimersByTime(1000)

    const keys = savedKeys().join(' ')
    expect(keys).not.toContain('temp')
    expect(keys).toContain('keep')
  })

  it('stores only the newest page of an infinite query', async () => {
    const m = await load()
    const c = client()
    m.adoptQueryCache(c, 'user-1')
    m.persistQueryCache(c)

    c.setQueryData(queryKeys.messages.list('c1'), { pages: ['newest', 'older', 'oldest'], pageParams: ['c', 'b', 'a'] })
    vi.advanceTimersByTime(1000)

    expect(stored()?.state.queries[0]?.state.data).toEqual({ pages: ['newest'], pageParams: ['c'] })
  })

  it('keeps only the five most recently fetched transcripts', async () => {
    const m = await load()
    const c = client()
    m.adoptQueryCache(c, 'user-1')
    m.persistQueryCache(c)

    for (let i = 0; i < 7; i++) {
      vi.setSystemTime(new Date(2026, 9, 10, 12, 0, i))
      c.setQueryData(queryKeys.messages.list(`c${i}`), pages({}))
    }
    vi.advanceTimersByTime(1000)

    const transcripts = savedKeys().filter((k) => k.includes('"messages"'))
    expect(transcripts).toHaveLength(5)
    expect(transcripts.join()).not.toContain('"c0"')
    expect(transcripts.join()).not.toContain('"c1"')
  })

  it('writes nothing for nobody', async () => {
    const m = await load()
    const c = client()
    m.adoptQueryCache(c, null)
    m.persistQueryCache(c)
    c.setQueryData(queryKeys.models.list(), [])
    vi.advanceTimersByTime(5000)
    expect(store.size).toBe(0)
  })

  it('saves at once when the page is hidden', async () => {
    const m = await load()
    const c = client()
    m.adoptQueryCache(c, 'user-1')
    m.persistQueryCache(c)
    c.setQueryData(queryKeys.models.list(), [])
    pagehide?.()
    expect(stored()?.owner).toBe('user-1')
  })

  it('survives storage that refuses to be written', async () => {
    const m = await load()
    vi.stubGlobal('localStorage', {
      getItem: () => null,
      setItem: () => {
        throw new Error('quota')
      },
      removeItem: () => {},
    })
    const c = client()
    m.adoptQueryCache(c, 'user-1')
    m.persistQueryCache(c)
    c.setQueryData(queryKeys.models.list(), [])
    expect(() => vi.advanceTimersByTime(1000)).not.toThrow()
  })
})

describe('restoreQueryCache', () => {
  const save = async (owner: string) => {
    const m = await load()
    const c = client()
    m.adoptQueryCache(c, owner)
    m.persistQueryCache(c)
    c.setQueryData(queryKeys.models.list(), [{ id: 'm' }])
    c.setQueryData(queryKeys.messages.list('c1'), pages({}))
    vi.advanceTimersByTime(1000)
    expect(stored()?.owner).toBe(owner)
    vi.resetModules()
  }

  it('puts the data back, dated as old as can be so it is refetched', async () => {
    await save('user-1')
    const m = await load()
    const c = client()
    m.restoreQueryCache(c)
    expect(c.getQueryData(queryKeys.models.list())).toEqual([{ id: 'm' }])
    expect(c.getQueryState(queryKeys.models.list())?.dataUpdatedAt).toBe(0)
  })

  it('keeps the cache for the account it belongs to', async () => {
    await save('user-1')
    const m = await load()
    const c = client()
    m.restoreQueryCache(c)
    m.adoptQueryCache(c, 'user-1')
    expect(c.getQueryData(queryKeys.models.list())).toBeDefined()
    expect(store.has(KEY)).toBe(true)
  })

  it('drops it for a different account', async () => {
    await save('user-1')
    const m = await load()
    const c = client()
    m.restoreQueryCache(c)
    m.adoptQueryCache(c, 'user-2')
    expect(c.getQueryData(queryKeys.models.list())).toBeUndefined()
    expect(c.getQueryData(queryKeys.messages.list('c1'))).toBeUndefined()
    expect(store.has(KEY)).toBe(false)
  })

  it('drops what the last account had beyond the stored queries, but not the session', async () => {
    await save('user-1')
    const m = await load()
    const chat = await import('@/stores/chat-store')
    const c = client()
    m.restoreQueryCache(c)
    c.setQueryData(queryKeys.memory.list(), ['theirs'])
    c.setQueryData(['admin', 'users'], ['theirs'])
    c.setQueryData(queryKeys.auth.session, { id: 'user-2' })
    chat.useChatStore.getState().post('c1', { content: 'theirs', attachments: [] })
    m.adoptQueryCache(c, 'user-2')
    expect(c.getQueryData(queryKeys.memory.list())).toBeUndefined()
    expect(c.getQueryData(['admin', 'users'])).toBeUndefined()
    expect(c.getQueryData(queryKeys.auth.session)).toEqual({ id: 'user-2' })
    expect(chat.useChatStore.getState().outbox).toEqual({})
  })

  it('does the same when the account changes without a reload', async () => {
    const m = await load()
    const c = client()
    m.adoptQueryCache(c, 'user-1')
    c.setQueryData(queryKeys.memory.list(), ['theirs'])
    m.adoptQueryCache(c, 'user-2')
    expect(c.getQueryData(queryKeys.memory.list())).toBeUndefined()
    // The same account again keeps what it has.
    c.setQueryData(queryKeys.memory.list(), ['mine'])
    m.adoptQueryCache(c, 'user-2')
    expect(c.getQueryData(queryKeys.memory.list())).toEqual(['mine'])
  })

  it('drops it when nobody is signed in', async () => {
    await save('user-1')
    const m = await load()
    const c = client()
    m.restoreQueryCache(c)
    m.adoptQueryCache(c, null)
    expect(c.getQueryData(queryKeys.models.list())).toBeUndefined()
    expect(store.has(KEY)).toBe(false)
  })

  it('starts from the network when storage is unreadable', async () => {
    store.set(KEY, '{not json')
    const m = await load()
    const c = client()
    expect(() => m.restoreQueryCache(c)).not.toThrow()
    expect(c.getQueryCache().getAll()).toHaveLength(0)
  })

  it('forgets everything on sign-out', async () => {
    await save('user-1')
    const m = await load()
    m.forgetQueryCache()
    expect(store.has(KEY)).toBe(false)
  })
})
