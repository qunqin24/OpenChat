import { OpenChatError, type ConversationEvent, type MessagePage } from '@openchat/api-client'
import { QueryClient } from '@tanstack/query-core'
import { describe, expect, it, vi } from 'vitest'
import { createStore } from 'zustand/vanilla'

import { chatStateCreator, type ChatState } from './chat-store'
import { followConversation, type FollowOptions } from './conversation-follow'
import { queryKeys } from './query-keys'
import { assistant, batch, id, run, user } from './testing'
import type { TranscriptPages } from './transcript-pages'

const conversationId = id('conv')

/** A connection that yields `events` and then stays open until aborted. */
function connection(events: ConversationEvent[]) {
  return async function* (signal: AbortSignal) {
    yield* events
    await new Promise<void>((resolve) => signal.addEventListener('abort', () => resolve()))
  }
}

function setup(over: Partial<FollowOptions> = {}) {
  const queryClient = new QueryClient()
  const store = createStore<ChatState>()(chatStateCreator)
  const controller = new AbortController()
  const options: FollowOptions = {
    conversationId,
    controller,
    events: connection([]),
    queryClient,
    store,
    fetchNewest: async () => ({ items: [], olderCursor: null }),
    leave: vi.fn(),
    unauthorized: vi.fn(),
    wait: () => Promise.resolve(),
    ...over,
  }
  return { queryClient, store, controller, options, done: followConversation(options) }
}

const tick = () => new Promise((resolve) => setTimeout(resolve, 0))

describe('followConversation', () => {
  it('hands a finished run over: the list holds its rows before the live copy goes', async () => {
    const r = run('r1')
    const q = user('q')
    const a = assistant('a', r.id)
    const finished = { ...r, status: 'completed' as const }
    const fresh: MessagePage = { items: [q, a], olderCursor: null }
    const { queryClient, store, controller, done } = setup({
      events: connection([
        { type: 'snapshot', messages: [] },
        batch(r, 2, [{ type: 'run_started', run: r, message: q }]),
        batch(r, 3, [{ type: 'message_appended', message: a }]),
        batch(r, 4, [{ type: 'run_finished', run: finished }]),
      ]),
      fetchNewest: async () => fresh,
    })
    await tick()
    await tick()
    expect(queryClient.getQueryData<TranscriptPages>(queryKeys.messages.list(conversationId))?.pages[0]).toEqual(fresh)
    expect(store.getState().live[conversationId]?.run ?? null).toBeNull()
    controller.abort()
    await done
  })

  it('leaves a conversation deleted elsewhere, and stops', async () => {
    const r = run('r1')
    const leave = vi.fn()
    const { done, controller } = setup({
      events: connection([{ type: 'snapshot', messages: [] }, batch(r, 2, [{ type: 'conversation_deleted' }])]),
      leave,
    })
    await done
    expect(leave).toHaveBeenCalledOnce()
    expect(controller.signal.aborted).toBe(true)
  })

  it('leaves a conversation the server does not know, and stops at a lost session', async () => {
    const notFound = new OpenChatError(404, { code: 'not_found', message: '' })
    const leave = vi.fn()
    await setup({
      events: async function* () {
        yield* []
        throw notFound
      },
      leave,
    }).done
    expect(leave).toHaveBeenCalledOnce()

    const signedOut = new OpenChatError(401, { code: 'unauthorized', message: '' })
    const unauthorized = vi.fn()
    await setup({
      events: async function* () {
        yield* []
        throw signedOut
      },
      unauthorized,
    }).done
    expect(unauthorized).toHaveBeenCalledWith(signedOut)
  })

  it('says once that the account was disabled, and does not reconnect', async () => {
    const disabled = new OpenChatError(403, { code: 'account_disabled', message: '', details: { reason: 'admin', disabledAt: '' } })
    const unauthorized = vi.fn()
    let connections = 0
    await setup({
      events: async function* () {
        connections++
        yield* []
        throw disabled
      },
      unauthorized,
    }).done
    expect(unauthorized).toHaveBeenCalledExactlyOnceWith(disabled)
    expect(connections).toBe(1)
  })

  it('reconnects after a dropped connection, from a fresh snapshot', async () => {
    let connections = 0
    const { controller, done } = setup({
      events: async function* (signal) {
        connections++
        if (connections < 3) throw new Error('dropped')
        yield { type: 'snapshot', messages: [] } as ConversationEvent
        await new Promise<void>((resolve) => signal.addEventListener('abort', () => resolve()))
      },
    })
    await tick()
    expect(connections).toBe(3)
    controller.abort()
    await done
  })
})
