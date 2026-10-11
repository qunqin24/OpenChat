import type { RunEvent } from '@openchat/api-client'
import { beforeEach, describe, expect, it } from 'vitest'

import { FINISHED_RUNS_KEPT, isRunning } from '@/lib/run-state'
import { assistant, batch, id, run, user } from '@/lib/testing'

import { resetChatStore, useChatStore } from './chat-store'

const conv = id('conv')
const state = () => useChatStore.getState()
const running = () => isRunning(state().live[conv])

const started = (r = run('r1'), input = user('q')): RunEvent => ({ type: 'run_started', run: r, message: input })

beforeEach(() => resetChatStore())

describe('a send response that arrives after the run is over', () => {
  it('does not put a finished, settled answer back to running', () => {
    const r = run('r1')
    const q = user('q')
    const done = run('r1', { status: 'completed', version: 4 })

    // The events stream tells all of it first...
    state().applyBatch(conv, batch(r, 2, [started(r, q)]))
    state().applyBatch(conv, batch(done, 4, [{ type: 'message_appended', message: assistant('a', r.id) }, { type: 'run_finished', run: done }]))
    // ...the message list is refreshed and the live copy let go...
    state().settle(conv, r.id)
    expect(state().live[conv]?.run).toBeNull()
    // ...and only then does the HTTP response of the send come back, saying "running".
    state().post(conv, { content: 'q', attachments: [] })
    state().startRun(conv, r, q)

    expect(state().live[conv]?.run).toBeNull()
    expect(running()).toBe(false)
    // The bubble waiting for the response is let go too: the rows are in the list.
    expect(state().outbox[conv]).toBeUndefined()
  })

  it('does so for a run that ended without the page ever seeing it start', () => {
    const r = run('r1')
    const done = run('r1', { status: 'completed', version: 4 })
    state().applyBatch(conv, batch(done, 4, [{ type: 'run_finished', run: done }]))
    state().startRun(conv, r, user('q'))
    expect(running()).toBe(false)
  })

  it('does not run again from a snapshot taken before the end', () => {
    const r = run('r1')
    const done = run('r1', { status: 'completed', version: 4 })
    state().applyBatch(conv, batch(r, 2, [started(r)]))
    state().applyBatch(conv, batch(done, 4, [{ type: 'run_finished', run: done }]))
    state().settle(conv, r.id)

    state().applySnapshot(conv, { type: 'snapshot', run: r, messages: [user('q')] })
    expect(running()).toBe(false)
    expect(state().live[conv]?.run).toBeNull()
  })

  it('does not announce a finished run a second time', () => {
    const r = run('r1')
    const done = run('r1', { status: 'completed', version: 4 })
    state().applyBatch(conv, batch(r, 2, [started(r)]))
    state().applyBatch(conv, batch(done, 4, [{ type: 'run_finished', run: done }]))
    state().settle(conv, r.id)

    state().applyBatch(conv, batch(r, 2, [started(r)]))
    expect(running()).toBe(false)
  })

  it('leaves a finished run that is not yet settled on screen', () => {
    const r = run('r1')
    const done = run('r1', { status: 'completed', version: 4 })
    state().applyBatch(conv, batch(r, 2, [started(r)]))
    state().applyBatch(conv, batch(done, 4, [{ type: 'run_finished', run: done }]))
    // The refresh failed, so it is still shown; the late response must not disturb it.
    state().startRun(conv, r, user('q'))
    expect(state().live[conv]?.run?.run.status).toBe('completed')
  })

  it('lets a later run start normally', () => {
    const r1 = run('r1')
    const done = run('r1', { status: 'completed', version: 4 })
    state().applyBatch(conv, batch(r1, 2, [started(r1)]))
    state().applyBatch(conv, batch(done, 4, [{ type: 'run_finished', run: done }]))
    state().settle(conv, r1.id)

    const r2 = run('r2')
    state().startRun(conv, r2, user('q2'))
    expect(state().live[conv]?.run?.run.id).toBe(r2.id)
    expect(running()).toBe(true)
  })
})

describe('the ordinary order', () => {
  it('starts a run from the response before any event, then follows the stream', () => {
    const r = run('r1')
    const q = user('q')
    state().post(conv, { content: 'q', attachments: [] })
    state().startRun(conv, r, q)
    expect(running()).toBe(true)
    expect(state().outbox[conv]).toBeUndefined()

    // The stream announcing the same run changes nothing, and finishing it ends it.
    state().applyBatch(conv, batch(r, 2, [started(r, q)]))
    expect(running()).toBe(true)
    const done = run('r1', { status: 'completed', version: 4 })
    state().applyBatch(conv, batch(done, 4, [{ type: 'run_finished', run: done }]))
    expect(running()).toBe(false)
    state().settle(conv, r.id)
    expect(state().live[conv]?.run).toBeNull()
  })

  it('starts a run from the stream before the response, and the response changes nothing', () => {
    const r = run('r1')
    const q = user('q')
    state().applyBatch(conv, batch(r, 2, [started(r, q)]))
    state().startRun(conv, r, q)
    expect(running()).toBe(true)
  })

  it('takes a snapshot of a run in flight', () => {
    const r = run('r1')
    state().applySnapshot(conv, { type: 'snapshot', run: r, messages: [user('q')] })
    expect(running()).toBe(true)
  })

  it('does not settle a run that is still running', () => {
    const r = run('r1')
    state().startRun(conv, r, user('q'))
    state().settle(conv, r.id)
    expect(running()).toBe(true)
    // And a run that was merely settled early is not remembered as finished.
    expect(state().finished).not.toContain(r.id)
  })
})

describe('the memory of finished runs', () => {
  it('is bounded, keeping the newest', () => {
    for (let i = 0; i < FINISHED_RUNS_KEPT + 10; i++) {
      const done = run(`f${i}`, { status: 'completed', version: 4 })
      state().applyBatch(conv, batch(done, 4, [{ type: 'run_finished', run: done }]))
    }
    expect(state().finished).toHaveLength(FINISHED_RUNS_KEPT)
    expect(state().finished).toContain(id(`f${FINISHED_RUNS_KEPT + 9}`))
    expect(state().finished).not.toContain(id('f0'))
  })

  it('is forgotten when the page is reset for someone else', () => {
    const done = run('r1', { status: 'completed', version: 4 })
    state().applyBatch(conv, batch(done, 4, [{ type: 'run_finished', run: done }]))
    expect(state().finished).toHaveLength(1)
    resetChatStore()
    expect(state().finished).toEqual([])
  })
})
