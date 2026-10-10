import { createMemoryHistory, createRootRoute, createRoute, createRouter, notFound } from '@tanstack/react-router'
import { beforeEach, describe, expect, it, vi } from 'vitest'

// The same guard the root route has (routes/__root.tsx) in a real router, to
// see what the router itself does with a preload: hovering a link must neither
// flip the mode nor log an error, and a click afterwards must still go.

type Mode = typeof import('./mode')
let mode: Mode
let chat: typeof import('@/stores/chat-store')
let query: typeof import('@/lib/query')

beforeEach(async () => {
  vi.resetModules()
  vi.stubGlobal('localStorage', { getItem: () => null, setItem: () => {}, removeItem: () => {} })
  mode = await import('./mode')
  chat = await import('@/stores/chat-store')
  query = await import('@/lib/query')
})

function build(start: string) {
  const seen: { path: string; preload: boolean }[] = []
  const root = createRootRoute({
    beforeLoad: ({ location, preload }) => {
      seen.push({ path: location.pathname, preload })
      if (!mode.enterPath(location.pathname, preload)) throw notFound()
    },
  })
  const page = (path: string) => createRoute({ getParentRoute: () => root, path })
  const router = createRouter({
    routeTree: root.addChildren([page('/'), page('/login'), page('/guest')]),
    history: createMemoryHistory({ initialEntries: [start] }),
  })
  return { router, seen }
}

describe('a link hovered in a real router', () => {
  it('does not flip a guest to account mode, nor report an error, and a click still goes', async () => {
    const errors = vi.spyOn(console, 'error').mockImplementation(() => {})
    const { router, seen } = build('/guest')
    await router.load()
    expect(mode.isGuestMode()).toBe(true)
    query.queryClient.setQueryData(['conversations', 'list'], ['mine'])
    chat.useChatStore.getState().post('c1', { content: 'hi', attachments: [] })

    // The pointer is over 「登录」.
    await router.preloadRoute({ to: '/login' } as never)
    expect(seen.at(-1)).toEqual({ path: '/login', preload: true })
    expect(mode.isGuestMode()).toBe(true)
    expect(query.queryClient.getQueryData(['conversations', 'list'])).toEqual(['mine'])
    expect(Object.keys(chat.useChatStore.getState().outbox)).toEqual(['c1'])
    expect(errors).not.toHaveBeenCalled()

    // And clicking it is a navigation: the mode changes then, and not before.
    await router.navigate({ to: '/login' } as never)
    expect(mode.isGuestMode()).toBe(false)
    expect(query.queryClient.getQueryData(['conversations', 'list'])).toBeUndefined()
    expect(router.state.location.pathname).toBe('/login')
    errors.mockRestore()
  })

  it('does not flip an account to guest mode when the guest button is hovered', async () => {
    const { router } = build('/login')
    await router.load()
    query.queryClient.setQueryData(['conversations', 'list'], ['mine'])
    await router.preloadRoute({ to: '/guest' } as never)
    expect(mode.isGuestMode()).toBe(false)
    expect(query.queryClient.getQueryData(['conversations', 'list'])).toEqual(['mine'])
    await router.navigate({ to: '/guest' } as never)
    expect(mode.isGuestMode()).toBe(true)
  })
})
