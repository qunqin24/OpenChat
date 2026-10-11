import type { ContextSummary, GuestCompactRequest, GuestEndEvent, GuestRunRequest, Memory, Message, Run } from '@openchat/api-client'
import { describe, expect, it } from 'vitest'

import { compactGuestConversation } from './compaction'
import { GuestHub } from './hub'
import { applyModelChanges, rewriteMemory } from './memory'
import { adoptMemoryReceipts, requestReceipts, signedSummary, withEndReceipts, withInstructionReceipts } from './receipts'
import { buildRunRequest } from './request'
import { GuestRunner } from './runner'
import { harness } from './testing'
import type { StoredMessage } from './types'

// The server's receipts for a guest's conversation (receipts.ts): kept with
// what they sign, sent back with it, and absent from old data without harm.

const iso = (n: number) => new Date(Date.parse('2026-10-11T00:00:00Z') + n * 1000).toISOString()

const sse = (frames: unknown[]) =>
  new Response(
    new ReadableStream({
      start(controller) {
        const encoder = new TextEncoder()
        for (const frame of frames) controller.enqueue(encoder.encode(`data: ${JSON.stringify(frame)}\n\n`))
        controller.close()
      },
    }),
    { headers: { 'Content-Type': 'text/event-stream' } },
  )

const memory = (over: Partial<Memory> = {}): Memory => ({
  id: 'm1', content: 'Likes tea.', source: 'user', createdAt: iso(1), updatedAt: iso(1), ...over,
})

describe('the pieces', () => {
  it('sends only the receipts it holds, and leaves out a summary the server did not sign', () => {
    expect(requestReceipts({ aboutYou: 'a', responseStyle: 'b', memoryEnabled: true })).toBeUndefined()
    expect(requestReceipts({ aboutYou: 'a', responseStyle: 'b', memoryEnabled: true, aboutYouReceipt: 'ra' }, 'rs')).toEqual({ aboutYou: 'ra', memorySnapshot: 'rs' })
    const summary: ContextSummary = { content: 's', throughMessageId: 'x', model: 'm', inputTokens: 1, outputTokens: 1, createdAt: iso(1) }
    expect(signedSummary(summary)).toBeUndefined()
    expect(signedSummary({ ...summary, receipt: 'r' })).toEqual({ ...summary, receipt: 'r' })
    expect(signedSummary(undefined)).toBeUndefined()
  })

  it('sets the receipts of the last frame on the rows and files they name, and only on those', () => {
    const row = (id: string, role: StoredMessage['role'], over: Partial<StoredMessage> = {}): StoredMessage => ({
      id, conversationId: 'c', role, content: id, parts: [], status: 'complete', createdAt: iso(1), ...over,
    })
    const rows = [
      row('q1', 'user', { receipt: 'old' }),
      row('a1', 'assistant', { receipt: 'a' }),
      row('q2', 'user', { attachments: [{ id: 'f1', filename: 'x.png', contentType: 'image/png', size: 1 }, { id: 'f2', filename: 'y.png', contentType: 'image/png', size: 1 }] }),
      row('q3', 'user'),
    ]
    const changed = withEndReceipts(rows, { messages: { q2: 'r2', a1: 'never' }, files: { f1: 'rf1' } })
    // The assistant's receipt came with the row and is not replaced; an unnamed row is untouched.
    expect(changed.map((m) => m.id)).toEqual(['q2'])
    expect(changed[0]).toMatchObject({ receipt: 'r2', attachments: [{ id: 'f1', receipt: 'rf1' }, { id: 'f2' }] })
    expect(changed[0]!.attachments![1]).not.toHaveProperty('receipt')
    expect(withEndReceipts(rows, undefined)).toEqual([])
    expect(withEndReceipts(rows, { messages: { q1: 'old' } })).toEqual([])
  })

  it('takes the server\'s receipt for a note only while the words are the same', () => {
    const list = [memory({ id: 'a' }), memory({ id: 'b', content: 'Edited since.' }), memory({ id: 'c', receipt: 'kept' })]
    const theirs = [memory({ id: 'a', receipt: 'ra' }), memory({ id: 'b', content: 'Likes tea.', receipt: 'rb' }), memory({ id: 'c', receipt: 'kept' })]
    expect(adoptMemoryReceipts(list, theirs).map((m) => [m.id, m.receipt])).toEqual([['a', 'ra']])
  })

  it('drops a note\'s receipt when its words change, and takes the one the server gave the model\'s', () => {
    const now = new Date(iso(5))
    expect(rewriteMemory([memory({ receipt: 'r' })], 'm1', 'Likes coffee.', now)).not.toHaveProperty('receipt')
    const list = [memory({ receipt: 'r', source: 'model' })]
    const event = { id: 'e', memoryId: 'm1', op: 'update' as const, before: 'Likes tea.', after: 'Likes coffee.', source: 'model' as const, createdAt: iso(6) }
    const signed = applyModelChanges(list, [event], [memory({ content: 'Likes coffee.', receipt: 'new', source: 'model' })])
    expect(signed.put[0]).toMatchObject({ content: 'Likes coffee.', receipt: 'new' })
    const unsigned = applyModelChanges(list, [event], [])
    expect(unsigned.put[0]).toMatchObject({ content: 'Likes coffee.' })
    expect(unsigned.put[0]).not.toHaveProperty('receipt')
  })

  it('keeps the instructions\' receipts the last frame brought', () => {
    const held = { aboutYou: 'a', responseStyle: 'b', memoryEnabled: true, responseStyleReceipt: 'old' }
    expect(withInstructionReceipts(held, { aboutYou: 'ra' })).toEqual({ ...held, aboutYouReceipt: 'ra' })
    expect(withInstructionReceipts(held, undefined)).toBe(held)
  })
})

describe('over a run', () => {
  function setup(frames: (request: GuestRunRequest, n: number) => unknown[]) {
    const h = harness()
    const requests: GuestRunRequest[] = []
    const runner = new GuestRunner({
      repo: h.repo,
      hub: new GuestHub(),
      baseUrl: '/api/v1',
      tab: 'tab',
      fetch: async (_input, init) => {
        const request = JSON.parse(String(init?.body)) as GuestRunRequest
        requests.push(request)
        return sse(frames(request, requests.length))
      },
    })
    return { h, runner, requests }
  }

  /** What the server streams for a send, receipts on. */
  function served(conversationId: string, request: GuestRunRequest, n: number): unknown[] {
    const runId = `run-${n}`
    const input = request.input!
    const run = (status: Run['status'], version: number): Run => ({
      id: runId, conversationId, status, model: 'model-a', version, createdAt: iso(n * 10 + 1), ...(status === 'running' ? {} : { finishedAt: iso(n * 10 + 5) }),
    })
    const answer: Message = {
      id: `a${n}`, conversationId, role: 'assistant', content: 'Hello', status: 'complete', runId, createdAt: iso(n * 10 + 4),
      stopReason: 'stop', parts: [{ type: 'text', text: 'Hello' }], model: 'model-a', providerState: 'cHM=', receipt: `receipt-a${n}`,
    }
    const end: GuestEndEvent = {
      type: 'guest_end', run: run('completed', 3), memorySnapshot: { about: 'Ada' },
      // The ids are the ones the request sent.
      memories: (request.memories ?? []).map((m) => ({ id: m.id, content: m.content, source: 'user' as const, createdAt: iso(1), updatedAt: iso(1), receipt: 'receipt-m1' })),
      memoryEvents: [],
      receipts: {
        aboutYou: 'receipt-about', responseStyle: 'receipt-style', memorySnapshot: 'receipt-snapshot',
        messages: { [input.id]: `receipt-${input.id}` },
        files: Object.fromEntries((input.attachments ?? []).map((f) => [f.id, `receipt-${f.id}`])),
      },
    }
    return [
      { type: 'batch', runId, version: 1, events: [{ type: 'run_started', run: run('running', 1), message: { ...input, conversationId, status: 'complete', createdAt: iso(n * 10 + 2), attachments: undefined, receipt: `receipt-${input.id}` } }] },
      { type: 'batch', runId, version: 3, events: [{ type: 'message_appended', message: answer }, { type: 'run_finished', run: run('completed', 3) }] },
      end,
    ]
  }

  it('keeps the receipts a run brings, and sends them back with what they sign', async () => {
    const t = setup((request, n) => served(request.conversationId, request, n))
    const c = await t.h.conversation()
    await t.h.repo.updatePersonalization({ aboutYou: 'Ada', responseStyle: 'terse' })
    await t.h.repo.createMemory('Likes tea.')
    await t.h.repo.putFile({ id: 'f1', conversationId: '', blob: new Blob(['x']), filename: 'x.png', contentType: 'image/png', size: 1 })

    // The first request holds nothing signed.
    await t.runner.start(c, { action: 'send', content: 'hi', attachmentIds: ['f1'], id: 'in-1' }, {})
    await t.runner.settled(c)
    expect(t.requests[0]).not.toHaveProperty('receipts')
    expect(t.requests[0]!.memories![0]).not.toHaveProperty('receipt')

    // What the run brought is kept, each with what it signs.
    const data = await t.h.repo.data(c)
    const question = data.messages.find((m) => m.id === 'in-1')!
    expect(question.receipt).toBe('receipt-in-1')
    expect(question.attachments![0]).toMatchObject({ id: 'f1', receipt: 'receipt-f1' })
    expect(data.messages.find((m) => m.id === 'a1')!.receipt).toBe('receipt-a1')
    expect(data.conversation).toMatchObject({ memorySnapshot: { about: 'Ada' }, memorySnapshotReceipt: 'receipt-snapshot' })
    expect(await t.h.repo.storedPersonalization()).toMatchObject({ aboutYouReceipt: 'receipt-about', responseStyleReceipt: 'receipt-style' })
    expect((await t.h.repo.memories())[0]).toMatchObject({ content: 'Likes tea.', receipt: 'receipt-m1' })

    // The page is not shown any of it.
    const page = await t.h.repo.listMessages(c, {})
    expect(JSON.stringify(page)).not.toContain('receipt')
    expect(JSON.stringify(await t.h.repo.getConversation(c))).not.toContain('receipt')
    expect(JSON.stringify(await t.h.repo.personalization())).not.toContain('receipt')

    // The next request sends them all back.
    await t.runner.start(c, { action: 'send', content: 'again', attachmentIds: [], id: 'in-2' }, {})
    await t.runner.settled(c)
    const next = t.requests[1]!
    expect(next.messages.map((m) => [m.id, m.receipt])).toEqual([['in-1', 'receipt-in-1'], ['a1', 'receipt-a1']])
    expect(next.messages[0]!.attachments![0]).toMatchObject({ id: 'f1', receipt: 'receipt-f1' })
    expect(next.memories![0]).toMatchObject({ content: 'Likes tea.', receipt: 'receipt-m1' })
    expect(next.receipts).toEqual({ aboutYou: 'receipt-about', responseStyle: 'receipt-style', memorySnapshot: 'receipt-snapshot' })
    // The new question has none: it is what the server is to check.
    expect(next.input).not.toHaveProperty('receipt')
  })

  it('does not send the receipt for words that were changed since, and the server will check them', async () => {
    const t = setup((request, n) => served(request.conversationId, request, n))
    const c = await t.h.conversation()
    await t.h.repo.updatePersonalization({ aboutYou: 'Ada', responseStyle: 'terse' })
    await t.runner.start(c, { action: 'send', content: 'hi', attachmentIds: [], id: 'in-1' }, {})
    await t.runner.settled(c)

    // The same words kept, and others written: only the first keeps its receipt.
    await t.h.repo.updatePersonalization({ responseStyle: 'verbose', memoryEnabled: true })
    expect(await t.h.repo.storedPersonalization()).toMatchObject({ aboutYouReceipt: 'receipt-about' })
    expect(await t.h.repo.storedPersonalization()).not.toHaveProperty('responseStyleReceipt')
    await t.h.repo.updatePersonalization({ aboutYou: 'Grace' })
    expect(await t.h.repo.storedPersonalization()).not.toHaveProperty('aboutYouReceipt')
  })

  it('does not keep the new message as refused when it was the conversation that was refused', async () => {
    let details: Record<string, unknown> = { categories: ['violence/graphic'] }
    const h = harness()
    const runner = new GuestRunner({
      repo: h.repo,
      hub: new GuestHub(),
      baseUrl: '/api/v1',
      tab: 'tab',
      fetch: async () => Response.json({ code: 'content_blocked', message: 'no', details }, { status: 422 }),
    })
    const c = await h.conversation()
    await expect(runner.start(c, { action: 'send', content: 'fine', attachmentIds: [], id: 'in-1' }, {})).rejects.toMatchObject({ code: 'content_blocked' })
    expect((await h.repo.data(c)).messages.map((m) => m.status)).toEqual(['blocked'])

    // An old row, a note or the instructions were what failed: the message was not.
    // (The refused message is reworded, as the conversation stops at it.)
    details = { categories: ['violence/graphic'], history: true }
    const blocked = (await h.repo.data(c)).messages[0]!
    await expect(runner.start(c, { action: 'edit', content: 'fine too', messageId: blocked.id, id: 'in-2' }, {})).rejects.toMatchObject({
      code: 'content_blocked', details: { history: true },
    })
    expect((await h.repo.data(c)).messages.map((m) => m.content)).toEqual(['fine'])
  })
})

describe('old data, from before receipts', () => {
  it('loads and sends as it did: no receipts anywhere, and a summary without one is left out', async () => {
    const h = harness()
    const c = await h.conversation()
    const first = await h.turn(c, { ask: 'one', answer: 'a1' })
    await h.turn(c, { ask: 'two', answer: 'a2' })
    await h.repo.setSummary(c, { content: '- old', throughMessageId: first.rows[0]!.id, model: 'm', inputTokens: 1, outputTokens: 1, createdAt: iso(1) })
    await h.repo.updatePersonalization({ aboutYou: 'Ada' })
    await h.repo.createMemory('Likes tea.')

    const built = await buildRunRequest(
      {
        data: await h.repo.data(c),
        personalization: await h.repo.storedPersonalization(),
        memories: await h.repo.memories(),
        readBlob: (id) => h.repo.file(id),
      },
      { action: 'send', content: 'three', id: 'in-3', attachmentIds: [] },
    )
    expect(built.request).not.toHaveProperty('receipts')
    expect(built.request).not.toHaveProperty('summary')
    // The rows it summarised are sent instead.
    expect(built.request.messages.map((m) => m.content)).toEqual(['one', 'a1', 'two', 'a2'])
    expect(JSON.stringify(built.request)).not.toContain('receipt')
    // And it all still reads for the page.
    expect((await h.repo.listMessages(c, {})).items).toHaveLength(4)
  })
})

describe('compressing on request', () => {
  it('sends the receipts of what it summarises, and keeps the one the summary comes with', async () => {
    const h = harness()
    const sent: GuestCompactRequest[] = []
    const deps = {
      repo: h.repo,
      hub: new GuestHub(),
      baseUrl: '/api/v1',
      now: () => new Date('2026-10-11T12:00:00Z'),
      fetch: async (_input: RequestInfo | URL, init?: RequestInit) => {
        sent.push(JSON.parse(String(init?.body)) as GuestCompactRequest)
        return Response.json({ content: '- new', model: 'model-a', inputTokens: 3, outputTokens: 2, receipt: 'receipt-summary' })
      },
    }
    const c = await h.conversation()
    const one = await h.turn(c, { ask: 'one', answer: 'a1' })
    await h.repo.messageAppended(c, { ...one.input, receipt: 'receipt-q1' })
    await h.repo.messageAppended(c, { ...one.rows[0]!, receipt: 'receipt-a1' })
    await h.repo.setSummary(c, { content: '- old', throughMessageId: one.rows[0]!.id, keptMessageId: one.input.id, model: 'm', inputTokens: 1, outputTokens: 1, createdAt: iso(1), receipt: 'receipt-old' })
    const two = await h.turn(c, { ask: 'two' })
    await h.repo.messageAppended(c, { ...two.input, receipt: 'receipt-q2' })

    const summary = await compactGuestConversation(deps, c)
    expect(sent[0]).toMatchObject({ existing: '- old', existingReceipt: 'receipt-old', latest: 'two', latestReceipt: 'receipt-q2' })
    expect(sent[0]!.messages.map((m) => [m.content, m.receipt])).toEqual([['one', 'receipt-q1']])
    expect(summary.receipt).toBe('receipt-summary')
    expect((await h.repo.compaction(c)).summary?.receipt).toBe('receipt-summary')
  })
})
