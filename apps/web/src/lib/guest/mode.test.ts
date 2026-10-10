import { beforeEach, describe, expect, it, vi } from 'vitest'

// The module keeps the mode in module state, so each test gets a fresh copy.
type Mode = typeof import('./mode')
type Chat = typeof import('@/stores/chat-store')
type Query = typeof import('@/lib/query')

let mode: Mode
let chat: Chat
let query: Query

beforeEach(async () => {
  vi.resetModules()
  vi.stubGlobal('localStorage', { getItem: () => null, setItem: () => {}, removeItem: () => {} })
  mode = await import('./mode')
  chat = await import('@/stores/chat-store')
  query = await import('@/lib/query')
})

/** Something a guest (or an account) would have on screen. */
function populate() {
  query.queryClient.setQueryData(['conversations', 'list'], ['kept'])
  chat.useChatStore.getState().post('c1', { content: 'hello', attachments: [] })
}
const held = () => ({
  cache: query.queryClient.getQueryData(['conversations', 'list']),
  outbox: Object.keys(chat.useChatStore.getState().outbox),
})

describe('modeOf', () => {
  it('puts /guest and everything under it in guest mode', () => {
    expect(mode.modeOf('/guest')).toBe('guest')
    expect(mode.modeOf('/guest/chat/1')).toBe('guest')
    expect(mode.modeOf('/guestbook')).toBe('account')
    expect(mode.modeOf('/login')).toBe('account')
  })
})

describe('enterPath', () => {
  it('changes the mode on a real navigation, emptying what the other mode held', () => {
    expect(mode.enterPath('/guest', false)).toBe(true)
    populate()
    expect(mode.enterPath('/login', false)).toBe(true)
    expect(mode.isGuestMode()).toBe(false)
    expect(held()).toEqual({ cache: undefined, outbox: [] })
  })

  it('leaves a guest alone when a link to the sign-in page is only hovered', () => {
    mode.enterPath('/guest', false)
    populate()
    // The preload of /login: refused, and nothing changed.
    expect(mode.enterPath('/login', true)).toBe(false)
    expect(mode.isGuestMode()).toBe(true)
    expect(held()).toEqual({ cache: ['kept'], outbox: ['c1'] })
  })

  it('leaves an account alone when the guest button is only hovered', () => {
    mode.enterPath('/login', false)
    populate()
    expect(mode.enterPath('/guest', true)).toBe(false)
    expect(mode.isGuestMode()).toBe(false)
    expect(held()).toEqual({ cache: ['kept'], outbox: ['c1'] })
  })

  it('still preloads pages of the mode it is in', () => {
    mode.enterPath('/guest', false)
    populate()
    expect(mode.enterPath('/guest/chat/1', true)).toBe(true)
    expect(held().cache).toEqual(['kept'])
    mode.enterPath('/login', false)
    expect(mode.enterPath('/chat/1', true)).toBe(true)
  })
})
