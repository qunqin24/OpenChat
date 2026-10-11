import type { Message, Run } from '@openchat/api-client'

import { GuestRepo, type StartedRun } from './repo'
import { memoryStorage } from './storage'

// Builders for the tests of this directory: a repository over maps, with a clock
// the test moves, and the calls the runner makes as the server's events arrive.

export interface Harness {
  repo: GuestRepo
  clock: { now: number }
  tick: (ms?: number) => string
  /** A conversation, made. */
  conversation: (over?: { temporary?: boolean; title?: string }) => Promise<string>
  /** A question and its answer, as a run writes them. Returns their ids. */
  turn: (conversationId: string, over: TurnOptions) => Promise<Turn>
}

export interface TurnOptions {
  /** The question's text; ignored when `regenerate` or `answer`. */
  ask?: string
  answer?: string
  action?: StartedRun['action']
  /** For an edit: the question it replaces, which goes with everything after it. */
  replaces?: string
  /** For a regeneration or an answer: the question it is about. */
  input?: string
  continues?: string
  status?: Run['status']
  usage?: Message['usage']
  tools?: boolean
}

export interface Turn {
  run: Run
  input: Message
  rows: Message[]
}

let counter = 0
const uuid = () => `00000000-0000-4000-8000-${String(++counter).padStart(12, '0')}`

/**
 * `skew` is how far the page's clock is ahead of the server's (negative: behind):
 * the rows `tick` stamps are the server's, and the repository's own `now` is the
 * page's.
 */
export function harness(skew = 0): Harness {
  const clock = { now: Date.parse('2026-10-10T10:00:00.000Z') }
  const tick = (ms = 1000) => new Date((clock.now += ms)).toISOString()
  const repo = new GuestRepo(memoryStorage(), {
    now: () => new Date(clock.now + skew),
    newId: uuid,
    urlFor: (_blob, id) => `blob:${id}`,
  })

  const turn: Harness['turn'] = async (conversationId, over) => {
    const action = over.action ?? 'send'
    const data = await repo.data(conversationId)
    let input: Message
    if (action === 'send') {
      input = {
        id: uuid(),
        conversationId,
        role: 'user',
        content: over.ask ?? 'question',
        parts: [{ type: 'text', text: over.ask ?? 'question' }],
        status: 'complete',
        createdAt: tick(),
      }
    } else {
      const found = data.messages.find((m) => m.id === over.input)
      if (!found) throw new Error('no such input')
      input = found as Message
    }
    const run: Run = {
      id: uuid(),
      conversationId,
      status: 'running',
      model: 'model-a',
      version: 1,
      createdAt: tick(),
      ...(over.continues ? { continues: over.continues } : {}),
    }
    await repo.runStarted(
      conversationId,
      {
        run,
        input,
        action,
        ...(over.replaces ? { replaces: over.replaces } : {}),
        choice: {},
      },
      'tab',
    )
    const rows: Message[] = []
    if (over.tools) {
      const call: Message = {
        id: uuid(), conversationId, role: 'assistant', content: '', status: 'complete', runId: run.id, createdAt: tick(),
        stopReason: 'tool_calls', parts: [{ type: 'tool_call', id: `call-${run.id}`, name: 'web_search', rawArguments: '{}' }],
      }
      const result: Message = {
        id: uuid(), conversationId, role: 'tool', content: '', status: 'complete', runId: run.id, createdAt: tick(),
        parts: [{ type: 'tool_result', callId: `call-${run.id}`, name: 'web_search', content: 'found', status: 'succeeded' }],
      }
      rows.push(call, result)
    }
    if (over.answer !== undefined) {
      rows.push({
        id: uuid(), conversationId, role: 'assistant', content: over.answer, status: 'complete', runId: run.id, createdAt: tick(),
        stopReason: 'stop', parts: [{ type: 'text', text: over.answer }], model: 'model-a', providerState: 'c2VjcmV0',
        ...(over.usage ? { usage: over.usage } : {}),
      })
    }
    for (const row of rows) await repo.messageAppended(conversationId, row)
    const finished: Run = { ...run, status: over.status ?? 'completed', version: 3, finishedAt: tick() }
    await repo.runUpdated(conversationId, finished)
    return { run: finished, input, rows }
  }

  return {
    repo,
    clock,
    tick,
    conversation: async (over = {}) => (await repo.createConversation({ temporary: over.temporary, title: over.title })).id,
    turn,
  }
}
