import type { BatchEvent, ContextSummary, ConversationEvent, GuestCompactRequest, GuestRunRequest, Run } from '@openchat/api-client'
import { describe, expect, it } from 'vitest'

import { compactGuestConversation, undoGuestCompaction, type CompactionDeps } from './compaction'
import { GuestHub } from './hub'
import { buildRunRequest, MAX_ROWS } from './request'
import { createGuestApi } from './router'
import { GuestRunner } from './runner'
import { harness } from './testing'

// A guest's compressed context: kept with the conversation, sent with a run,
// kept again from the run's events, made on request through the server's model,
// and gone with the rows it is tied to.

const summaryOf = (through: string, over: Partial<ContextSummary> = {}): ContextSummary => ({
  content: '- earlier things',
  throughMessageId: through,
  model: 'model-a',
  inputTokens: 10,
  outputTokens: 4,
  createdAt: '2026-10-10T00:00:00Z',
  receipt: 'signed-by-the-server',
  ...over,
})

const run = (id: string, status: Run['status'] = 'completed'): Run => ({
  id, conversationId: 'c', status, model: 'model-a', version: 2, createdAt: '2026-10-10T00:00:00Z',
})

describe('the summary kept with a conversation', () => {
  it('is read back while the rows it is tied to are there, and not shown as part of the conversation', async () => {
    const h = harness()
    const c = await h.conversation()
    const t1 = await h.turn(c, { ask: 'one', answer: 'a1' })
    const answer = t1.rows[0]!
    expect(await h.repo.setSummary(c, summaryOf(answer.id))).toBeDefined()
    expect((await h.repo.compaction(c)).summary?.content).toBe('- earlier things')
    expect(await h.repo.getConversation(c)).not.toHaveProperty('summary')
  })

  it('is not kept for rows that are not there', async () => {
    const h = harness()
    const c = await h.conversation()
    await h.turn(c, { ask: 'one', answer: 'a1' })
    expect(await h.repo.setSummary(c, summaryOf('gone'))).toBeUndefined()
    expect((await h.repo.compaction(c)).summary).toBeUndefined()
  })

  it('goes with an edit of a question inside its range, and stays through one after it', async () => {
    const h = harness()
    const c = await h.conversation()
    const t1 = await h.turn(c, { ask: 'one', answer: 'a1' })
    const t2 = await h.turn(c, { ask: 'two', answer: 'a2' })
    await h.repo.setSummary(c, summaryOf(t1.rows[0]!.id))

    // Editing the second question, which is after the range: the summary stays.
    await h.turn(c, { ask: 'two, reworded', answer: 'a2b', replaces: t2.input.id })
    expect((await h.repo.compaction(c)).summary).toBeDefined()

    // Editing the first, inside it: the summary goes with the rows.
    await h.turn(c, { ask: 'one, reworded', answer: 'a1b', replaces: t1.input.id })
    expect((await h.repo.compaction(c)).summary).toBeUndefined()
    expect((await h.repo.data(c)).conversation.summary).toBeNull()
  })

  it('goes with an answer that was regenerated and covered, and stays when it ended at the question', async () => {
    const h = harness()
    const c = await h.conversation()
    const t1 = await h.turn(c, { ask: 'one', answer: 'a1' })
    await h.repo.setSummary(c, summaryOf(t1.rows[0]!.id))
    await h.turn(c, { action: 'regenerate', input: t1.input.id, answer: 'a1 again' })
    expect((await h.repo.compaction(c)).summary).toBeUndefined()

    // A summary that ends at the question (the answer failed for its length).
    const t2 = await h.turn(c, { ask: 'two', answer: 'a2' })
    await h.repo.setSummary(c, summaryOf(t2.input.id, { keptMessageId: t2.input.id }))
    await h.turn(c, { action: 'regenerate', input: t2.input.id, answer: 'a2 again' })
    expect((await h.repo.compaction(c)).summary).toBeDefined()
  })

  it('is replaced, and taken back, by setting it', async () => {
    const h = harness()
    const c = await h.conversation()
    const t1 = await h.turn(c, { ask: 'one', answer: 'a1' })
    await h.repo.setSummary(c, summaryOf(t1.rows[0]!.id, { content: 'first' }))
    await h.repo.setSummary(c, summaryOf(t1.rows[0]!.id, { content: 'second' }))
    expect((await h.repo.compaction(c)).summary?.content).toBe('second')
    await h.repo.setSummary(c, undefined)
    expect((await h.repo.compaction(c)).summary).toBeUndefined()
  })
})

describe('the request of a run', () => {
  const build = async (h: ReturnType<typeof harness>, c: string, what: Parameters<typeof buildRunRequest>[1]) =>
    buildRunRequest(
      {
        data: await h.repo.data(c),
        personalization: { aboutYou: '', responseStyle: '', memoryEnabled: true },
        memories: [],
        readBlob: async () => undefined,
      },
      what,
    )

  it('carries the summary, and only the rows it needs', async () => {
    const h = harness()
    const c = await h.conversation()
    const t1 = await h.turn(c, { ask: 'one', answer: 'a1' })
    const t2 = await h.turn(c, { ask: 'two', answer: 'a2' })
    await h.turn(c, { ask: 'three', answer: 'a3' })
    // The summary ends at the second answer and kept the second question.
    await h.repo.setSummary(c, summaryOf(t2.rows[0]!.id, { keptMessageId: t2.input.id }))

    const { request } = await build(h, c, { action: 'send', content: 'four', attachmentIds: [], id: 'in-4' })
    expect(request.summary?.throughMessageId).toBe(t2.rows[0]!.id)
    // The kept question, the row it ends at, then what follows; not the first turn, which the summary replaces.
    expect(request.messages.map((m) => m.content)).toEqual(['two', 'a2', 'three', 'a3'])
    expect(request.messages.map((m) => m.content)).not.toContain(t1.input.content)
  })

  it('sends none for a conversation that has none, and the newest rows', async () => {
    const h = harness()
    const c = await h.conversation()
    await h.turn(c, { ask: 'one', answer: 'a1' })
    const { request } = await build(h, c, { action: 'send', content: 'two', attachmentIds: [], id: 'in-2' })
    expect(request.summary).toBeUndefined()
    expect(request.messages).toHaveLength(2)
    expect(MAX_ROWS).toBeGreaterThan(2)
  })

  it('leaves out a summary the run is about to delete the rows of', async () => {
    const h = harness()
    const c = await h.conversation()
    const t1 = await h.turn(c, { ask: 'one', answer: 'a1' })
    const t2 = await h.turn(c, { ask: 'two', answer: 'a2' })
    await h.repo.setSummary(c, summaryOf(t2.rows[0]!.id))

    // Editing the second question deletes it and what follows: the answer the summary ends at.
    const edit = await build(h, c, { action: 'edit', content: 'two, reworded', messageId: t2.input.id, id: 'in-3' })
    expect(edit.request.summary).toBeUndefined()
    expect(edit.request.messages.map((m) => m.content)).toEqual(['one', 'a1'])

    // Asking the last question again deletes its answer, which the summary ends at.
    const again = await build(h, c, { action: 'regenerate' })
    expect(again.request.summary).toBeUndefined()

    // A summary that ends before the edit stays.
    await h.repo.setSummary(c, summaryOf(t1.rows[0]!.id))
    const kept = await build(h, c, { action: 'edit', content: 'two, reworded', messageId: t2.input.id, id: 'in-4' })
    expect(kept.request.summary?.throughMessageId).toBe(t1.rows[0]!.id)
  })
})

describe('a run that compresses', () => {
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

  it('keeps the summary as it is told, and tells the page', async () => {
    const h = harness()
    const hub = new GuestHub()
    const c = await h.conversation()
    const made = summaryOf('in-1', { keptMessageId: 'in-1', content: 'made by the run' })
    const runner = new GuestRunner({
      repo: h.repo,
      hub,
      baseUrl: '/api/v1',
      tab: 'tab',
      fetch: async (_input, init) => {
        const request = JSON.parse(String(init?.body)) as GuestRunRequest
        const input = request.input!
        const running = { ...run('run-1', 'running'), conversationId: c, version: 1 }
        const done = { ...run('run-1'), conversationId: c, version: 4, finishedAt: '2026-10-11T00:00:09Z' }
        return sse([
          { type: 'batch', runId: 'run-1', version: 1, events: [{ type: 'run_started', run: running, message: { ...input, conversationId: c, status: 'complete', createdAt: '2026-10-11T00:00:01Z', attachments: undefined } }] },
          { type: 'batch', runId: 'run-1', version: 2, events: [{ type: 'compaction_started' }] },
          { type: 'batch', runId: 'run-1', version: 3, events: [{ type: 'compaction_finished', summary: made }] },
          { type: 'batch', runId: 'run-1', version: 4, events: [{ type: 'run_finished', run: done }] },
          { type: 'guest_end', run: done, memorySnapshot: null, memories: [], memoryEvents: [], summary: made },
        ])
      },
    })
    const seen: ConversationEvent[] = []
    hub.subscribe(c, (e) => seen.push(e))

    await runner.start(c, { action: 'send', content: 'hi', attachmentIds: [], id: 'in-1' }, {})
    await runner.settled(c)

    expect((await h.repo.compaction(c)).summary).toMatchObject({ content: 'made by the run', throughMessageId: 'in-1', keptMessageId: 'in-1' })
    const kinds = seen.flatMap((e) => (e.type === 'batch' ? e.events.map((x) => x.type) : [e.type]))
    expect(kinds).toEqual(['run_started', 'compaction_started', 'compaction_finished', 'run_finished'])
  })

  it('keeps the summary the last frame hands back, even without having been told', async () => {
    const h = harness()
    const hub = new GuestHub()
    const c = await h.conversation()
    const made = summaryOf('in-1', { keptMessageId: 'in-1', content: 'handed back' })
    const runner = new GuestRunner({
      repo: h.repo,
      hub,
      baseUrl: '/api/v1',
      tab: 'tab',
      fetch: async (_input, init) => {
        const input = (JSON.parse(String(init?.body)) as GuestRunRequest).input!
        const done = { ...run('run-1'), conversationId: c, version: 3, finishedAt: '2026-10-11T00:00:09Z' }
        return sse([
          { type: 'batch', runId: 'run-1', version: 1, events: [{ type: 'run_started', run: { ...done, status: 'running', version: 1 }, message: { ...input, conversationId: c, status: 'complete', createdAt: '2026-10-11T00:00:01Z', attachments: undefined } }] },
          { type: 'batch', runId: 'run-1', version: 3, events: [{ type: 'run_finished', run: done }] },
          { type: 'guest_end', run: done, memorySnapshot: null, memories: [], memoryEvents: [], summary: made },
        ])
      },
    })
    await runner.start(c, { action: 'send', content: 'hi', attachmentIds: [], id: 'in-1' }, {})
    await runner.settled(c)
    expect((await h.repo.compaction(c)).summary?.content).toBe('handed back')
  })

  it('shows a page that joins while it compresses the status', () => {
    const hub = new GuestHub()
    const started: BatchEvent = {
      type: 'batch', runId: 'run-1', version: 1,
      events: [{ type: 'run_started', run: { ...run('run-1', 'running'), version: 1 }, message: { id: 'q', conversationId: 'c', role: 'user', content: 'q', parts: [], status: 'complete', createdAt: '2026-10-11T00:00:00Z' } }],
    }
    hub.publish('c', started)
    hub.publish('c', { type: 'batch', runId: 'run-1', version: 2, events: [{ type: 'compaction_started' }] })
    expect(hub.snapshot('c').live?.compacting).toBe(true)
    hub.publish('c', { type: 'batch', runId: 'run-1', version: 3, events: [{ type: 'compaction_finished' }] })
    expect(hub.snapshot('c').live?.compacting).toBeUndefined()
  })
})

describe('compressing on request', () => {
  function setup(respond: (request: GuestCompactRequest) => Response | Promise<Response>) {
    const h = harness()
    const hub = new GuestHub()
    const sent: GuestCompactRequest[] = []
    const changed: string[] = []
    const seen: ConversationEvent[] = []
    const deps: CompactionDeps = {
      repo: h.repo,
      hub,
      baseUrl: '/api/v1',
      changed: (id) => changed.push(id),
      now: () => new Date('2026-10-11T12:00:00Z'),
      fetch: async (input, init) => {
        expect(String(input)).toBe('/api/v1/guest/compaction')
        const body = JSON.parse(String(init?.body)) as GuestCompactRequest
        sent.push(body)
        return respond(body)
      },
    }
    return { h, hub, deps, sent, changed, seen, listen: (id: string) => hub.subscribe(id, (e) => seen.push(e)) }
  }
  const written = (content = '- the summary') => Response.json({ content, model: 'model-a', inputTokens: 30, outputTokens: 8 })

  it('summarises the whole conversation, keeps the summary, and tells the page and other pages', async () => {
    const t = setup(() => written())
    const c = await t.h.conversation()
    t.listen(c)
    await t.h.turn(c, { ask: 'one', answer: 'a1' })
    const t2 = await t.h.turn(c, { ask: 'two', answer: 'a2' })
    await t.h.repo.updateConversation(c, { model: 'model-a' })

    const summary = await compactGuestConversation(t.deps, c)
    expect(summary).toMatchObject({
      content: '- the summary', throughMessageId: t2.rows[0]!.id, model: 'model-a', inputTokens: 30, outputTokens: 8,
      createdAt: '2026-10-11T12:00:00.000Z',
    })
    expect(summary.keptMessageId).toBeUndefined()
    expect(t.sent[0]).toMatchObject({ model: 'model-a' })
    expect(t.sent[0]!.messages.map((m) => m.content)).toEqual(['one', 'a1', 'two', 'a2'])
    expect(t.sent[0]).not.toHaveProperty('existing')
    expect((await t.h.repo.compaction(c)).summary?.content).toBe('- the summary')
    expect(t.changed).toEqual([c])
    expect(t.seen.flatMap((e) => (e.type === 'batch' ? e.events.map((x) => x.type) : []))).toEqual(['compaction_changed'])
  })

  it('folds the old summary and what came after into the new one', async () => {
    const t = setup(() => written('- the new summary'))
    const c = await t.h.conversation()
    const t1 = await t.h.turn(c, { ask: 'one', answer: 'a1' })
    await t.h.repo.setSummary(c, summaryOf(t1.rows[0]!.id, { keptMessageId: t1.input.id, content: '- the old summary' }))
    await t.h.turn(c, { ask: 'two', answer: 'a2' })

    await compactGuestConversation(t.deps, c)
    expect(t.sent[0]!.existing).toBe('- the old summary')
    // The question the old summary kept was never written into it.
    expect(t.sent[0]!.messages.map((m) => m.content)).toEqual(['one', 'two', 'a2'])
    expect((await t.h.repo.compaction(c)).summary?.content).toBe('- the new summary')
  })

  it('keeps a question that has no answer, and sends it only as the latest message', async () => {
    const t = setup(() => written())
    const c = await t.h.conversation()
    await t.h.turn(c, { ask: 'one', answer: 'a1' })
    const failed = await t.h.turn(c, { ask: 'two', status: 'failed' })

    const summary = await compactGuestConversation(t.deps, c)
    expect(summary.throughMessageId).toBe(failed.input.id)
    expect(summary.keptMessageId).toBe(failed.input.id)
    expect(t.sent[0]!.latest).toBe('two')
    expect(t.sent[0]!.messages.map((m) => m.content)).toEqual(['one', 'a1'])
  })

  it('refuses while a run is going, and when there is nothing new', async () => {
    const t = setup(() => written())
    const c = await t.h.conversation()
    await expect(compactGuestConversation(t.deps, c)).rejects.toMatchObject({ status: 409, code: 'nothing_to_compact' })

    const t1 = await t.h.turn(c, { ask: 'one', answer: 'a1' })
    await compactGuestConversation(t.deps, c)
    await expect(compactGuestConversation(t.deps, c)).rejects.toMatchObject({ code: 'nothing_to_compact' })

    // A run going: the conversation is its.
    await t.h.repo.runStarted(c, { run: { ...run('r2', 'running'), conversationId: c }, input: t1.input, action: 'regenerate', choice: {} }, 'tab')
    await expect(compactGuestConversation(t.deps, c)).rejects.toMatchObject({ status: 409, code: 'conversation_busy' })
    await expect(undoGuestCompaction(t.deps, c)).rejects.toMatchObject({ status: 409, code: 'conversation_busy' })
  })

  it('passes the server\'s refusal on as it was, and writes nothing', async () => {
    const t = setup(() => Response.json({ code: 'guest_ip_limit', message: 'slow down' }, { status: 429 }))
    const c = await t.h.conversation()
    await t.h.turn(c, { ask: 'one', answer: 'a1' })
    await expect(compactGuestConversation(t.deps, c)).rejects.toMatchObject({ status: 429, code: 'guest_ip_limit' })
    expect((await t.h.repo.compaction(c)).summary).toBeUndefined()
    expect(t.changed).toEqual([])
  })

  it('does not keep a summary of rows that an edit deleted while the model was writing it', async () => {
    let edit = async () => {}
    const t = setup(async () => {
      await edit()
      return written()
    })
    const c = await t.h.conversation()
    const t1 = await t.h.turn(c, { ask: 'one', answer: 'a1' })
    edit = async () => {
      await t.h.turn(c, { ask: 'one, reworded', answer: 'a1b', replaces: t1.input.id })
    }
    await expect(compactGuestConversation(t.deps, c)).rejects.toMatchObject({ status: 409 })
    expect((await t.h.repo.compaction(c)).summary).toBeUndefined()
  })

  it('takes the compression back: the originals were never removed', async () => {
    const t = setup(() => written())
    const c = await t.h.conversation()
    t.listen(c)
    await t.h.turn(c, { ask: 'one', answer: 'a1' })
    await compactGuestConversation(t.deps, c)
    const rows = (await t.h.repo.data(c)).messages.length

    await undoGuestCompaction(t.deps, c)
    expect((await t.h.repo.compaction(c)).summary).toBeUndefined()
    expect((await t.h.repo.data(c)).messages).toHaveLength(rows)
    expect(t.seen.flatMap((e) => (e.type === 'batch' ? e.events : [])).filter((e) => e.type === 'compaction_changed')).toHaveLength(2)

    // Idempotent, and silent when there was nothing to take back.
    t.changed.length = 0
    await undoGuestCompaction(t.deps, c)
    expect(t.changed).toEqual([])
  })
})

describe('the guest API', () => {
  it('answers the compaction routes of the contract', async () => {
    const h = harness()
    const hub = new GuestHub()
    const runner = new GuestRunner({ repo: h.repo, hub, baseUrl: '/api/v1', tab: 'tab', fetch: async () => new Response(null, { status: 500 }) })
    const api = createGuestApi({
      repo: h.repo, runner, hub, baseUrl: '/api/v1', config: async () => ({}) as never,
      fetch: async () => Response.json({ content: '- the summary', model: 'model-a', inputTokens: 1, outputTokens: 1 }),
    })
    const c = await h.conversation()
    await h.turn(c, { ask: 'one', answer: 'a1' })
    const call = async (method: string) => api.handle(new Request(`http://x/api/v1/conversations/${c}/compaction`, { method }), `/conversations/${c}/compaction`)

    expect(await (await call('GET'))!.json()).toEqual({})
    const made = await call('POST')
    expect(made!.status).toBe(200)
    expect(((await made!.json()) as { summary: ContextSummary }).summary.content).toBe('- the summary')
    expect(((await (await call('GET'))!.json()) as { summary?: ContextSummary }).summary?.content).toBe('- the summary')
    expect((await call('DELETE'))!.status).toBe(204)
    expect(await (await call('GET'))!.json()).toEqual({})
    const again = await call('POST')
    expect(again!.status).toBe(200)
    // Nothing more to compress now.
    expect((await call('POST'))!.status).toBe(409)
  })
})
