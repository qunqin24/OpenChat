import type { Attachment } from '@openchat/api-client'
import { describe, expect, it } from 'vitest'

import { applyBatch, fromSnapshot, idle, isRunning, type ConversationLive } from './run-state'
import { assistant, batch, id, liveRun, run, text, user } from './testing'

const started = (r = run('r1'), input = user('q')) => ({ type: 'run_started' as const, run: r, message: input })

describe('applyBatch', () => {
  it('starts a run from run_started', () => {
    const r = run('r1')
    const q = user('q')
    const state = applyBatch(idle, batch(r, 2, [started(r, q)]))
    expect(state.run?.run.id).toBe(r.id)
    expect(state.run?.run.version).toBe(2)
    expect(state.run?.messages).toEqual([q])
    expect(isRunning(state)).toBe(true)
  })

  it('ignores events for a run it never saw start', () => {
    const r = run('r1')
    const state = applyBatch(idle, batch(r, 2, [{ type: 'generation_started', attempt: 1 }]))
    expect(state).toBe(idle)
  })

  it('skips a batch it already holds, and one from the past', () => {
    const r = run('r1', { version: 5 })
    const held: ConversationLive = { run: liveRun(r, [user('q')]) }
    expect(applyBatch(held, batch(r, 5, [{ type: 'generation_started', attempt: 1 }]))).toBe(held)
    expect(applyBatch(held, batch(r, 4, [{ type: 'generation_started', attempt: 1 }]))).toBe(held)
  })

  it('does not mutate the state it was given', () => {
    const r = run('r1')
    const held: ConversationLive = { run: liveRun(r, [user('q')]) }
    const frozen = JSON.stringify(held)
    applyBatch(held, batch(r, 2, [{ type: 'generation_started', attempt: 1 }, { type: 'part_started', index: 0, part: text('') }]))
    expect(JSON.stringify(held)).toBe(frozen)
  })

  describe('run_started keeps what the event cannot carry', () => {
    const attachment: Attachment = { id: id('att'), filename: 'a.png', contentType: 'image/png', size: 4, url: 'https://x.test/a.png' }

    it('keeps the attachments of the copy the send response gave', () => {
      const r = run('r1')
      const sent = user('q', { attachments: [attachment] })
      const held: ConversationLive = { run: liveRun(r, [sent]) }
      // The event's copy is the same message, without the signed links.
      const state = applyBatch(held, batch(r, 2, [started(r, user('q'))]))
      expect(state.run?.messages[0]?.attachments).toEqual([attachment])
    })

    it('does not invent attachments the held copy lacked', () => {
      const r = run('r1')
      const held: ConversationLive = { run: liveRun(r, [user('q', { attachments: [] })]) }
      const state = applyBatch(held, batch(r, 2, [started(r, user('q'))]))
      expect(state.run?.messages[0]).not.toHaveProperty('attachments')
    })

    it('takes the event’s message when the held run is a different one', () => {
      const old = run('r0')
      const held: ConversationLive = { run: liveRun(old, [user('q', { attachments: [attachment] })]) }
      const r = run('r1')
      const state = applyBatch(held, batch(r, 1, [started(r, user('q'))]))
      expect(state.run?.run.id).toBe(r.id)
      expect(state.run?.messages[0]?.attachments).toBeUndefined()
    })

    it('resets the partial, retry and tools of a restarted run', () => {
      const r = run('r1')
      const held: ConversationLive = {
        run: liveRun(r, [user('q')], { partial: [text('x')], tools: { c: 'running' }, retry: { at: 'now', errorCode: 'e' } }),
      }
      const state = applyBatch(held, batch(r, 2, [started(r, user('q'))]))
      expect(state.run).toMatchObject({ partial: null, retry: null, tools: {} })
    })
  })

  describe('streaming a response', () => {
    const r = run('r1')
    const base = (): ConversationLive => ({ run: liveRun(r, [user('q')]) })

    it('builds the partial from part events', () => {
      const state = applyBatch(
        base(),
        batch(r, 2, [
          { type: 'generation_started', attempt: 1 },
          { type: 'part_started', index: 0, part: text('') },
          { type: 'part_delta', index: 0, delta: 'Hel' },
          { type: 'part_delta', index: 0, delta: 'lo' },
          { type: 'part_started', index: 1, part: { type: 'tool_call', id: 'c', name: 'web_search', rawArguments: '' } },
          { type: 'part_delta', index: 1, delta: '{"q":' },
          { type: 'part_delta', index: 1, delta: '1}' },
        ]),
      )
      expect(state.run?.partial).toEqual([
        text('Hello'),
        { type: 'tool_call', id: 'c', name: 'web_search', rawArguments: '{"q":1}' },
      ])
    })

    it('ignores a delta for a part that has not started', () => {
      const state = applyBatch(base(), batch(r, 2, [{ type: 'generation_started', attempt: 1 }, { type: 'part_delta', index: 3, delta: 'x' }]))
      expect(state.run?.partial).toEqual([])
    })

    it('replaces the part when it ends', () => {
      const state = applyBatch(
        base(),
        batch(r, 2, [
          { type: 'generation_started', attempt: 1 },
          { type: 'part_started', index: 0, part: text('') },
          { type: 'part_delta', index: 0, delta: 'draft' },
          { type: 'part_ended', index: 0, part: text('final') },
        ]),
      )
      expect(state.run?.partial).toEqual([text('final')])
    })

    it('turns the partial into a message when the assistant row is appended, once', () => {
      const row = assistant('a', r.id)
      const events = [
        { type: 'generation_started' as const, attempt: 1 },
        { type: 'part_started' as const, index: 0, part: text('a') },
        { type: 'message_appended' as const, message: row },
      ]
      const state = applyBatch(base(), batch(r, 2, events))
      expect(state.run?.partial).toBeNull()
      expect(state.run?.messages.map((m) => m.id)).toEqual([id('q'), row.id])

      // The same row again (a replayed batch) is not added twice.
      const again = applyBatch(state, batch(r, 3, [{ type: 'message_appended', message: row }]))
      expect(again.run?.messages).toHaveLength(2)
    })

    it('keeps the partial when a tool row is appended', () => {
      const tool = { ...assistant('t', r.id), role: 'tool' as const }
      const state = applyBatch(
        base(),
        batch(r, 2, [{ type: 'generation_started', attempt: 1 }, { type: 'part_started', index: 0, part: text('x') }, { type: 'message_appended', message: tool }]),
      )
      expect(state.run?.partial).toEqual([text('x')])
    })

    it('tracks tools and retries', () => {
      const state = applyBatch(
        base(),
        batch(r, 2, [
          { type: 'tool_started', callId: 'c1', name: 'web_search' },
          { type: 'tool_started', callId: 'c2', name: 'web_fetch' },
          { type: 'tool_finished', callId: 'c1', messageId: id('tm') },
        ]),
      )
      expect(state.run?.tools).toEqual({ c1: 'done', c2: 'running' })

      const retried = applyBatch(
        { run: { ...state.run!, partial: [text('lost')] } },
        batch(r, 3, [{ type: 'retry_scheduled', attempt: 1, at: '2026-10-10T00:00:05Z', errorCode: 'upstream' }]),
      )
      expect(retried.run?.partial).toBeNull()
      expect(retried.run?.retry).toEqual({ at: '2026-10-10T00:00:05Z', errorCode: 'upstream' })
    })

    it('finishes the run, keeping its rows, and stops running', () => {
      const done = { ...r, status: 'completed' as const, version: 9 }
      const state = applyBatch(
        { run: liveRun(r, [user('q')], { partial: [text('x')] }) },
        batch(done, 9, [{ type: 'run_finished', run: done }]),
      )
      expect(state.run?.run.status).toBe('completed')
      expect(state.run?.partial).toBeNull()
      expect(state.run?.messages).toHaveLength(1)
      expect(isRunning(state)).toBe(false)
    })

    it('raises the version to the batch’s', () => {
      const state = applyBatch(base(), batch(r, 12, [{ type: 'generation_started', attempt: 1 }]))
      expect(state.run?.run.version).toBe(12)
    })
  })
})

describe('fromSnapshot', () => {
  it('is idle without a run', () => {
    expect(fromSnapshot({ type: 'snapshot', messages: [] })).toBe(idle)
  })

  it('takes the generation in flight and the tool slots', () => {
    const r = run('r1')
    const q = user('q')
    const state = fromSnapshot({
      type: 'snapshot',
      run: r,
      messages: [q],
      live: {
        generation: { attempt: 1, parts: [text('so far')] },
        tools: [{ callId: 'c', name: 'web_search', status: 'running' }],
      },
    })
    expect(state.run).toMatchObject({ run: r, messages: [q], partial: [text('so far')], tools: { c: 'running' } })
  })

  it('has no partial for an empty generation', () => {
    const state = fromSnapshot({
      type: 'snapshot',
      run: run('r1'),
      messages: [],
      live: { generation: { attempt: 1, parts: [] } },
    })
    expect(state.run?.partial).toBeNull()
  })
})

describe('isRunning', () => {
  it('is false for no state, an idle one and a finished run', () => {
    expect(isRunning(undefined)).toBe(false)
    expect(isRunning(idle)).toBe(false)
    expect(isRunning({ run: liveRun(run('r1', { status: 'awaiting_input' }), []) })).toBe(false)
  })
})
