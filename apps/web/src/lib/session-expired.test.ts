import type { OpenChatError } from '@openchat/api-client'
import { beforeEach, describe, expect, it, vi } from 'vitest'

type Expired = typeof import('./session-expired')
type Query = typeof import('./query')
type Chat = typeof import('@/stores/chat-store')

let expired: Expired
let query: Query
let chat: Chat
// Built from the same copy of the module the code under test sees: after a
// reset, an error of the earlier copy's class would not be an `instanceof` it.
let unauthorized: OpenChatError
let wrongPassword: OpenChatError
let failure: (status: number, code: string) => OpenChatError

beforeEach(async () => {
  vi.resetModules()
  vi.stubGlobal('localStorage', { getItem: () => null, setItem: () => {}, removeItem: () => {} })
  expired = await import('./session-expired')
  query = await import('./query')
  chat = await import('@/stores/chat-store')
  const client = await import('@openchat/api-client')
  failure = (status, code) => new client.OpenChatError(status, { code, message: code })
  unauthorized = failure(401, 'unauthorized')
  wrongPassword = failure(401, 'invalid_credentials')
})

function fakeRouter(pathname = '/chat/1') {
  const navigate = vi.fn(() => Promise.resolve())
  return { navigate, latestLocation: { pathname, href: `${pathname}?x=1` } }
}

/** A page that believes it is signed in, with something of theirs on screen. */
function signedIn() {
  query.queryClient.setQueryData(['auth', 'session'], { id: 'u1' })
  query.queryClient.setQueryData(['conversations', 'list'], ['theirs'])
  chat.useChatStore.getState().post('c1', { content: 'hello', attachments: [] })
}

describe('isSessionExpired', () => {
  it('is a 401 from the account API', () => {
    expect(expired.isSessionExpired(unauthorized)).toBe(true)
  })
  it('is not the sign-in form’s wrong password, nor any other failure', () => {
    expect(expired.isSessionExpired(wrongPassword)).toBe(false)
    expect(expired.isSessionExpired(failure(403, 'forbidden'))).toBe(false)
    expect(expired.isSessionExpired(failure(500, 'x'))).toBe(false)
    expect(expired.isSessionExpired(new Error('network'))).toBe(false)
  })
})

describe('leaveIfSessionExpired', () => {
  it('drops everything and goes to the sign-in page, returning to where the person was', async () => {
    signedIn()
    const router = fakeRouter()
    await expired.leaveIfSessionExpired(unauthorized, router)
    expect(query.queryClient.getQueryData(['conversations', 'list'])).toBeUndefined()
    expect(query.queryClient.getQueryData(['auth', 'session'])).toBeUndefined()
    expect(chat.useChatStore.getState().outbox).toEqual({})
    expect(router.navigate).toHaveBeenCalledWith({ to: '/login', search: { redirect: '/chat/1?x=1' }, replace: true })
  })

  it('is what a failed query reaches through the cache', async () => {
    signedIn()
    const router = fakeRouter()
    query.setErrorListener((error) => void expired.leaveIfSessionExpired(error, router))
    await query.queryClient
      .fetchQuery({
        queryKey: ['conversations', 'detail', 'x'],
        queryFn: () => Promise.reject(unauthorized),
        retry: false,
      })
      .catch(() => undefined)
    await vi.waitFor(() => expect(router.navigate).toHaveBeenCalledTimes(1))
  })

  it('does nothing for the sign-in form’s own wrong password', async () => {
    signedIn()
    const router = fakeRouter('/login')
    await expired.leaveIfSessionExpired(wrongPassword, router)
    expect(router.navigate).not.toHaveBeenCalled()
    expect(query.queryClient.getQueryData(['conversations', 'list'])).toEqual(['theirs'])
  })

  it('does not loop on the sign-in and sign-up pages', async () => {
    signedIn()
    for (const path of ['/login', '/register']) {
      const router = fakeRouter(path)
      await expired.leaveIfSessionExpired(unauthorized, router)
      expect(router.navigate).not.toHaveBeenCalled()
    }
  })

  it('leaves the verdict to the route guard when no session was ever believed in', async () => {
    const router = fakeRouter()
    await expired.leaveIfSessionExpired(unauthorized, router)
    expect(router.navigate).not.toHaveBeenCalled()
    query.queryClient.setQueryData(['auth', 'session'], null)
    await expired.leaveIfSessionExpired(unauthorized, router)
    expect(router.navigate).not.toHaveBeenCalled()
  })

  it('goes once however many calls find out together', async () => {
    signedIn()
    const router = fakeRouter()
    await Promise.all([
      expired.leaveIfSessionExpired(unauthorized, router),
      expired.leaveIfSessionExpired(unauthorized, router),
      expired.leaveIfSessionExpired(unauthorized, router),
    ])
    expect(router.navigate).toHaveBeenCalledTimes(1)
  })
})
