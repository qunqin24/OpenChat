import type { BatchEvent, ConversationEvent, GuestEndEvent, GuestRunRequest, Message, Run, RunEvent } from '@openchat/api-client'
import { describe, expect, it } from 'vitest'

import { GuestHub } from './hub'
import { buildRunRequest, MAX_FILE_BYTES, MAX_ROWS } from './request'
import { createGuestApi, apiPath } from './router'
import { GuestRunner } from './runner'
import { harness } from './testing'

// The runner against a server that is a script: what it streams back is kept
// before it is told, the last frame is kept, a lost connection is a failed run,
// a refusal writes nothing. And the request it builds says what the server
// needs to run the conversation.

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

const iso = (n: number) => new Date(Date.parse('2026-10-11T00:00:00Z') + n * 1000).toISOString()

/** What the server streams for a send that is answered. */
function answered(conversationId: string, request: GuestRunRequest, opts: { end?: boolean; runId?: string } = {}): unknown[] {
  const runId = opts.runId ?? 'run-1'
  // Later runs happen later, as the server's clock has it.
  const k = ((Number(runId.replace('run-', '')) || 1) - 1) * 10
  // A regeneration has no new question: the server answers the last one it was sent.
  const input = request.input ?? [...request.messages].reverse().find((m) => m.role === 'user')!
  const run = (status: Run['status'], version: number): Run => ({
    id: runId, conversationId, status, model: 'model-a', version, createdAt: iso(k + 1),
    ...(status === 'running' ? {} : { finishedAt: iso(k + 5) }),
  })
  const row: Message = {
    id: `answer-${runId.replace('run-', '')}`, conversationId, role: 'assistant', content: 'Hello', status: 'complete', runId, createdAt: iso(k + 4),
    stopReason: 'stop', parts: [{ type: 'text', text: 'Hello' }], model: 'model-a', providerState: 'cHM=',
    usage: { inputTokens: 5, outputTokens: 2 },
  }
  const frames: unknown[] = [
    {
      type: 'batch', runId, version: 1,
      events: [{ type: 'run_started', run: run('running', 1), message: { ...input, conversationId, status: 'complete', createdAt: iso(k + 2), attachments: undefined } }],
    },
    { type: 'batch', runId, version: 2, events: [{ type: 'generation_started', attempt: 1 }, { type: 'part_started', index: 0, part: { type: 'text', text: '' } }, { type: 'part_delta', index: 0, delta: 'Hel' }] },
    { type: 'batch', runId, version: 3, events: [{ type: 'message_appended', message: row }, { type: 'run_finished', run: run('completed', 3) }] },
  ]
  if (opts.end !== false) {
    const end: GuestEndEvent = {
      type: 'guest_end', run: run('completed', 3), memorySnapshot: { about: 'hi' },
      memories: [{ id: 'm1', content: 'Likes tea.', source: 'model', createdAt: iso(4), updatedAt: iso(4) }],
      memoryEvents: [{ id: 'e1', memoryId: 'm1', op: 'add', after: 'Likes tea.', source: 'model', createdAt: iso(4) }],
      title: 'A greeting',
    }
    frames.push(end)
  }
  return frames
}

function setup(respond: (request: GuestRunRequest, url: string) => Response | Promise<Response>) {
  const h = harness()
  const hub = new GuestHub()
  const requests: GuestRunRequest[] = []
  const settled: string[] = []
  const deleted: string[] = []
  const runner = new GuestRunner({
    repo: h.repo,
    hub,
    baseUrl: '/api/v1',
    tab: 'tab',
    onSettled: (id) => settled.push(id),
    onDeleted: (id) => deleted.push(id),
    fetch: async (input, init) => {
      const url = String(input)
      const body = init?.body ? (JSON.parse(String(init.body)) as GuestRunRequest) : ({} as GuestRunRequest)
      if (url.endsWith('/guest/runs')) requests.push(body)
      return respond(body, url)
    },
  })
  const seen: ConversationEvent[] = []
  return { h, hub, runner, requests, seen, settled, deleted, listen: (id: string) => hub.subscribe(id, (e) => seen.push(e)) }
}

describe('a send', () => {
  it('keeps each row before it is told, and the last frame after the run is told finished', async () => {
    const t = setup((request) => sse(answered(request.conversationId, request)))
    const c = await t.h.conversation()
    t.listen(c)

    const started = await t.runner.start(c, { action: 'send', content: 'hi', attachmentIds: [], id: 'in-1' }, { model: 'model-a' })
    expect(started.run.id).toBe('run-1')
    expect(started.inputId).toBe('in-1')
    // Kept as soon as the server has taken it.
    expect((await t.h.repo.data(c)).messages.map((m) => m.id)).toEqual(['in-1'])

    await t.runner.settled(c)
    const page = await t.h.repo.listMessages(c, {})
    expect(page.items.map((m) => `${m.role}:${m.content}`)).toEqual(['user:hi', 'assistant:Hello'])
    const data = await t.h.repo.data(c)
    expect(data.runs[0]).toMatchObject({ id: 'run-1', status: 'completed', inputId: 'in-1' })
    expect(data.conversation).toMatchObject({ title: 'A greeting', model: 'model-a', memorySnapshot: { about: 'hi' } })
    expect((await t.h.repo.memories()).map((m) => m.content)).toEqual(['Likes tea.'])
    expect((await t.h.repo.memoryEvents()).map((e) => e.id)).toEqual(['e1'])
    // What a provider wants back stays with the row, out of the page's reach.
    expect(data.messages.find((m) => m.id === 'answer-1')!.providerState).toBe('cHM=')

    const kinds = t.seen.flatMap((e) => (e.type === 'batch' ? e.events.map((x: RunEvent) => x.type) : [e.type]))
    expect(kinds).toEqual(['run_started', 'generation_started', 'part_started', 'part_delta', 'message_appended', 'run_finished', 'title_changed'])
    expect(t.settled).toEqual([c])
    // The hub holds the finished run as live state, as the page's store would.
    expect(t.hub.live(c).run?.run.status).toBe('completed')
  })

  it('hands a page that connects mid-run the snapshot of the run', async () => {
    let release = () => {}
    const gate = new Promise<void>((resolve) => (release = resolve))
    const t = setup((request) => {
      const frames = answered(request.conversationId, request)
      return new Response(
        new ReadableStream({
          async start(controller) {
            const encoder = new TextEncoder()
            const send = (f: unknown) => controller.enqueue(encoder.encode(`data: ${JSON.stringify(f)}\n\n`))
            send(frames[0])
            send(frames[1])
            await gate
            frames.slice(2).forEach(send)
            controller.close()
          },
        }),
      )
    })
    const c = await t.h.conversation()
    await t.runner.start(c, { action: 'send', content: 'hi', attachmentIds: [], id: 'in-1' }, {})
    await new Promise((r) => setTimeout(r, 10))
    const snapshot = t.hub.snapshot(c)
    expect(snapshot.run?.id).toBe('run-1')
    expect(snapshot.messages.map((m) => m.id)).toEqual(['in-1'])
    expect(snapshot.live?.generation?.parts).toEqual([{ type: 'text', text: 'Hel' }])
    expect(t.runner.isRunning(c)).toBe(true)
    release()
    await t.runner.settled(c)
    expect(t.runner.isRunning(c)).toBe(false)
    expect(t.hub.snapshot(c)).toEqual({ type: 'snapshot', messages: [] })
  })

  it('writes nothing when the server refuses, and passes its error on as it was', async () => {
    const t = setup(() => Response.json({ code: 'guest_ip_limit', message: 'slow down' }, { status: 429 }))
    const c = await t.h.conversation()
    await expect(t.runner.start(c, { action: 'send', content: 'hi', attachmentIds: [], id: 'in-1' }, {})).rejects.toMatchObject({
      status: 429,
      code: 'guest_ip_limit',
    })
    expect((await t.h.repo.data(c)).messages).toEqual([])
    expect(t.runner.isRunning(c)).toBe(false)
  })

  it('records a stream that ends without its last frame as a failed run', async () => {
    const t = setup((request) => sse(answered(request.conversationId, request).slice(0, 2)))
    const c = await t.h.conversation()
    t.listen(c)
    await t.runner.start(c, { action: 'send', content: 'hi', attachmentIds: [], id: 'in-1' }, {})
    await t.runner.settled(c)

    const page = await t.h.repo.listMessages(c, {})
    expect(page.items.map((m) => `${m.role}:${m.status}:${m.errorCode ?? ''}`)).toEqual(['user:complete:', 'assistant:failed:connection_lost'])
    const last = t.seen.at(-1) as BatchEvent
    expect(last.events.map((e) => e.type)).toEqual(['message_appended', 'run_finished'])
    expect((await t.h.repo.data(c)).runs[0]).toMatchObject({ status: 'failed', errorCode: 'connection_lost' })
  })

  it('waits for the previous run\'s last frame before asking again, so the new request carries its memories', async () => {
    let n = 0
    const t = setup((request) => sse(answered(request.conversationId, request, { runId: `run-${++n}` })))
    const c = await t.h.conversation()
    await t.runner.start(c, { action: 'send', content: 'one', attachmentIds: [], id: 'in-1' }, {})
    await t.runner.start(c, { action: 'send', content: 'two', attachmentIds: [], id: 'in-2' }, {})
    const second = t.requests[1]!
    expect(second.memories).toEqual([{ id: 'm1', content: 'Likes tea.', source: 'model' }])
    expect(second.memorySnapshot).toEqual({ about: 'hi' })
    expect(second.titled).toBe(true)
    expect(second.messages.map((m) => m.id)).toEqual(['in-1', 'answer-1'])
    expect(second.messages[1]!.providerState).toBe('cHM=')
    await t.runner.settled(c)
  })
})

describe('a redo', () => {
  /** A server that answers each request as a new run (run-1, run-2, ...). */
  const serving = () => {
    let n = 0
    return setup((request) => sse(answered(request.conversationId, request, { runId: `run-${++n}` })))
  }
  const texts = async (t: ReturnType<typeof setup>, c: string) =>
    (await t.h.repo.listMessages(c, {})).items.map((m) => `${m.role}:${m.content}`)

  it('deletes an edited question and everything after it once the server has taken the new one', async () => {
    const t = serving()
    const c = await t.h.conversation()
    t.listen(c)
    await t.runner.start(c, { action: 'send', content: 'one', attachmentIds: [], id: 'in-1' }, {})
    await t.runner.settled(c)
    await t.runner.start(c, { action: 'send', content: 'two', attachmentIds: [], id: 'in-2' }, {})
    await t.runner.settled(c)
    expect(await texts(t, c)).toEqual(['user:one', 'assistant:Hello', 'user:two', 'assistant:Hello'])

    // In-1 reworded: the server is asked with nothing before it, and other pages are told.
    await t.runner.start(c, { action: 'edit', content: 'one, reworded', messageId: 'in-1', id: 'in-3' }, {})
    await t.runner.settled(c)
    expect(t.requests.at(-1)!.messages).toEqual([])
    expect(t.requests.at(-1)!.input).toMatchObject({ id: 'in-3', content: 'one, reworded' })
    expect(t.deleted).toEqual([c])

    expect(await texts(t, c)).toEqual(['user:one, reworded', 'assistant:Hello'])
    const data = await t.h.repo.data(c)
    // Nothing of the questions that went is left: not their rows, not their runs.
    expect(data.messages.map((m) => m.id).sort()).toEqual(['answer-3', 'in-3'])
    expect(data.runs.map((r) => r.inputId)).toEqual(['in-3'])
  })

  it('deletes the old answer, runs and rows, when an answer is made again', async () => {
    const t = serving()
    const c = await t.h.conversation()
    await t.runner.start(c, { action: 'send', content: 'one', attachmentIds: [], id: 'in-1' }, {})
    await t.runner.settled(c)
    expect((await t.h.repo.data(c)).runs.map((r) => r.id)).toEqual(['run-1'])

    await t.runner.start(c, { action: 'regenerate' }, {})
    await t.runner.settled(c)
    // The question is asked again on its own.
    expect(t.requests.at(-1)!.messages.map((m) => m.id)).toEqual(['in-1'])
    expect(await texts(t, c)).toEqual(['user:one', 'assistant:Hello'])
    const data = await t.h.repo.data(c)
    expect(data.runs.map((r) => r.id)).toEqual(['run-2'])
    expect(data.messages).toHaveLength(2)
  })

  it('keeps the old rows when the server refuses the redo', async () => {
    let refuse = false
    const t = setup((request) =>
      refuse ? Response.json({ code: 'no_models', message: 'none' }, { status: 409 }) : sse(answered(request.conversationId, request)),
    )
    const c = await t.h.conversation()
    await t.runner.start(c, { action: 'send', content: 'one', attachmentIds: [], id: 'in-1' }, {})
    await t.runner.settled(c)
    refuse = true
    await expect(t.runner.start(c, { action: 'edit', content: 'x', messageId: 'in-1', id: 'in-2' }, {})).rejects.toMatchObject({ code: 'no_models' })
    await expect(t.runner.start(c, { action: 'regenerate' }, {})).rejects.toMatchObject({ code: 'no_models' })
    expect(await texts(t, c)).toEqual(['user:one', 'assistant:Hello'])
  })
})

describe('the guest API', () => {
  it('answers the contract\'s routes from the page and leaves the rest to the network', async () => {
    const t = setup((request) => sse(answered(request.conversationId, request)))
    const api = createGuestApi({
      repo: t.h.repo,
      runner: t.runner,
      hub: t.hub,
      baseUrl: '/api/v1',
      fetch: async () => new Response(null),
      config: async () => ({ models: [], features: { webSearch: false, avatarUploads: false }, personalization: { memoryAllowed: true, instructionLimit: 1500, memoryLimit: 100, memoryLength: 200 } }),
      newId: (() => { let i = 0; return () => `id-${++i}` })(),
    })
    const call = async (method: string, path: string, body?: unknown) => {
      const response = await api.handle(new Request(`http://page.test/api/v1${path}`, { method, ...(body ? { method, body: JSON.stringify(body), headers: { 'Content-Type': 'application/json' } } : {}) }), path.split('?')[0]!)
      return response && { status: response.status, body: response.status === 204 ? null : await response.json() }
    }

    const created = await call('POST', '/conversations', { title: 'Mine' })
    expect(created?.status).toBe(201)
    const id = (created!.body as { id: string }).id

    const sent = await call('POST', `/conversations/${id}/messages`, { content: '  hello  ' })
    expect(sent?.status).toBe(202)
    expect((sent!.body as { message: { id: string; content: string }; run: Run }).message).toMatchObject({ content: 'hello' })
    await t.runner.settled(id)

    const list = await call('GET', `/conversations/${id}/messages?limit=10`)
    expect((list!.body as { items: Message[] }).items.map((m) => m.role)).toEqual(['user', 'assistant'])
    expect((await call('GET', '/conversations/running'))?.body).toEqual({ ids: [] })
    expect((await call('GET', `/conversations/${id}/context`))?.body).toEqual({ usedTokens: 7, model: 'model-a' })
    expect((await call('POST', `/conversations/${id}/messages`, { content: '   ' }))).toMatchObject({ status: 400, body: { code: 'empty_content' } })
    expect((await call('GET', '/conversations/nope'))).toMatchObject({ status: 404 })
    expect((await call('GET', '/auth/session'))).toMatchObject({ status: 401 })
    expect((await call('POST', '/files', { filename: 'a.exe', contentType: 'application/x-msdownload', size: 3 }))).toMatchObject({ status: 415, body: { code: 'unsupported_file_type' } })
    expect((await call('POST', '/files', { filename: 'a.png', contentType: 'image/png', size: 3 }))?.status).toBe(201)
    expect((await call('POST', '/me/memories', { content: 'Likes rice' }))?.status).toBe(201)
    expect(((await call('GET', '/me/memories'))!.body as { items: unknown[] }).items).toHaveLength(2)
    expect((await call('DELETE', `/conversations/${id}`))?.status).toBe(204)
    // A call this API does not serve is the network's.
    expect(await call('GET', '/admin/system')).toBeNull()
  })

  it('finds the path under the API\'s base URL', () => {
    expect(apiPath('http://a.test/api/v1/conversations?x=1', '/api/v1', 'http://a.test')).toBe('/conversations')
    expect(apiPath('/api/v1/models', '/api/v1', 'http://a.test')).toBe('/models')
    expect(apiPath('http://a.test/elsewhere', '/api/v1', 'http://a.test')).toBeNull()
    expect(apiPath('http://other.test/api/v1/models', '/api/v1', 'http://a.test')).toBeNull()
  })
})

describe('the request', () => {
  const personalization = { aboutYou: 'Ada', responseStyle: 'terse', memoryEnabled: true }
  const base = async (h: ReturnType<typeof harness>, c: string) => ({
    data: await h.repo.data(c),
    personalization,
    memories: [],
    readBlob: (id: string) => h.repo.file(id),
  })

  it('carries the conversation and what the model is asked, for each action', async () => {
    const h = harness()
    const c = await h.conversation()
    await h.turn(c, { ask: 'q1', answer: 'a1' })
    const q2 = await h.turn(c, { ask: 'q2', answer: 'a2' })

    const send = await buildRunRequest({ ...(await base(h, c)), model: 'm', webSearch: false }, { action: 'send', content: 'q3', id: 'new', attachmentIds: [] })
    expect(send.request).toMatchObject({ action: 'send', conversationId: c, model: 'm', webSearch: false, aboutYou: 'Ada', titled: false })
    expect(send.request.messages.map((m) => m.content)).toEqual(['q1', 'a1', 'q2', 'a2'])
    expect(send.request.input).toMatchObject({ id: 'new', content: 'q3' })
    expect(send.input).toMatchObject({ id: 'new' })
    expect(send.input?.replaces).toBeUndefined()

    // An edit ends the request before the question, and says which it replaces.
    const edit = await buildRunRequest(await base(h, c), { action: 'edit', content: 'q2 again', messageId: q2.input.id, id: 'edit' })
    expect(edit.request.action).toBe('send')
    expect(edit.request.messages.map((m) => m.content)).toEqual(['q1', 'a1'])
    expect(edit.input?.replaces).toBe(q2.input.id)
    await expect(buildRunRequest(await base(h, c), { action: 'edit', content: 'x', messageId: 'nope', id: 'e' })).rejects.toMatchObject({ code: 'message_not_found' })

    // Regenerating asks for the last question again, without its answer.
    const regenerate = await buildRunRequest(await base(h, c), { action: 'regenerate' })
    expect(regenerate.request.messages.map((m) => m.content)).toEqual(['q1', 'a1', 'q2'])
    expect(regenerate.request.input).toBeUndefined()
  })

  it('answers the question the last row asks, joining the run that began the reply', async () => {
    const h = harness()
    const c = await h.conversation()
    const q = await h.turn(c, { ask: 'q', answer: '', status: 'awaiting_input' })
    // The model's question: the last row, an ask_user call.
    const asking: Message = {
      id: 'ask', conversationId: c, role: 'assistant', content: '', status: 'complete', runId: q.run.id, createdAt: h.tick(),
      stopReason: 'tool_calls', parts: [{ type: 'tool_call', id: 'call-q', name: 'ask_user', rawArguments: '{}' }],
    }
    await h.repo.messageAppended(c, asking)

    const answer = await buildRunRequest(await base(h, c), { action: 'answer', callId: 'call-q', answers: [{ selected: ['Beach'] }] })
    expect(answer.request).toMatchObject({ action: 'answer', callId: 'call-q', continues: q.run.id, answers: [{ selected: ['Beach'] }] })
    expect(answer.request.messages.at(-1)!.id).toBe('ask')
    await expect(buildRunRequest(await base(h, c), { action: 'answer', callId: 'other', answers: [] })).rejects.toMatchObject({ code: 'not_pending' })
  })

  it('sends the newest files and only names the ones that no longer fit', async () => {
    const h = harness()
    const c = await h.conversation()
    const small = new Blob([new Uint8Array(3)], { type: 'image/png' })
    for (const id of ['old', 'new']) {
      await h.repo.putFile({ id, conversationId: '', blob: small, filename: `${id}.png`, contentType: 'image/png', size: id === 'old' ? MAX_FILE_BYTES - 5 : 10 })
    }
    // Sent with two messages: the older is nearly the whole budget.
    const first = await buildRunRequest(await base(h, c), { action: 'send', content: 'one', id: 'm1', attachmentIds: ['old'] })
    await h.repo.runStarted(c, {
      run: { id: 'r1', conversationId: c, status: 'completed', model: 'm', version: 2, createdAt: h.tick() },
      input: { id: 'm1', conversationId: c, role: 'user', content: 'one', parts: [], status: 'complete', createdAt: h.tick() },
      action: 'send', attachments: first.input!.blobs, choice: {},
    }, 't')
    const second = await buildRunRequest(await base(h, c), { action: 'send', content: 'two', id: 'm2', attachmentIds: ['new'] })

    const sentFiles = [...second.request.messages, second.request.input!].flatMap((m) => m.attachments ?? [])
    expect(sentFiles.map((f) => [f.id, f.data !== undefined])).toEqual([['old', false], ['new', true]])
    await expect(buildRunRequest(await base(h, c), { action: 'send', content: 'x', id: 'm3', attachmentIds: ['ghost'] })).rejects.toMatchObject({ code: 'attachment_unavailable' })
  })

  it('carries at most the last rows of a long conversation', async () => {
    const h = harness()
    const c = await h.conversation()
    for (let i = 0; i < MAX_ROWS; i++) await h.turn(c, { ask: `q${i}`, answer: `a${i}` })
    const built = await buildRunRequest(await base(h, c), { action: 'send', content: 'next', id: 'n', attachmentIds: [] })
    expect(built.request.messages).toHaveLength(MAX_ROWS)
    expect(built.request.messages.at(-1)!.content).toBe(`a${MAX_ROWS - 1}`)
  })
})
