import type { ConversationEvent } from '@openchat/api-client'
import { describe, expect, it } from 'vitest'

import { GuestHub } from './hub'
import { createGuestApi } from './router'
import { GuestRunner } from './runner'
import { harness } from './testing'

/**
 * Deleting a guest's conversation tells whoever follows it - this page's open
 * stream, and the guest's other tabs - as the server's `conversation_deleted`
 * does for an account.
 */
function setup() {
  const h = harness()
  const hub = new GuestHub()
  const runner = new GuestRunner({ repo: h.repo, hub, baseUrl: '/api/v1', tab: 'tab', fetch: async () => new Response(null, { status: 500 }) })
  const told: string[][] = []
  const api = createGuestApi({
    repo: h.repo, runner, hub, baseUrl: '/api/v1', config: async () => ({}) as never,
    fetch: async () => new Response(null, { status: 500 }),
    conversationsDeleted: (ids) => told.push(ids),
  })
  const heard = (conversationId: string) => {
    const events: ConversationEvent[] = []
    hub.subscribe(conversationId, (event) => events.push(event))
    return () => events.flatMap((event) => (event.type === 'batch' ? event.events.map((e) => e.type) : []))
  }
  return { h, api, told, heard }
}

describe('deleting a guest conversation', () => {
  it('tells its followers and the other tabs', async () => {
    const { h, api, told, heard } = setup()
    const c = await h.conversation()
    const types = heard(c)
    const res = await api.handle(new Request(`http://x/api/v1/conversations/${c}`, { method: 'DELETE' }), `/conversations/${c}`)
    expect(res!.status).toBe(204)
    expect(types()).toEqual(['conversation_deleted'])
    expect(told).toEqual([[c]])
  })

  it('deleting everything tells each conversation', async () => {
    const { h, api, told, heard } = setup()
    const a = await h.conversation()
    const b = await h.conversation()
    const [typesA, typesB] = [heard(a), heard(b)]
    const res = await api.handle(new Request('http://x/api/v1/me/conversations', { method: 'DELETE' }), '/me/conversations')
    expect(res!.status).toBe(200)
    expect(typesA()).toEqual(['conversation_deleted'])
    expect(typesB()).toEqual(['conversation_deleted'])
    expect(told.flat().sort()).toEqual([a, b].sort())
  })
})
