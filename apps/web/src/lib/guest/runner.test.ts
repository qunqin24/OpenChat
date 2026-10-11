import type { BatchEvent, ConversationEvent, GuestEndEvent, GuestRunRequest, Message, Run, RunEvent } from '@openchat/api-client'
import { describe, expect, it, vi } from 'vitest'

import { GuestHub } from './hub'
import { buildRunRequest, MAX_FILE_BYTES, MAX_ROWS, type RunAction } from './request'
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

function setup(respond: (request: GuestRunRequest, url: string) => Response | Promise<Response>, skew = 0) {
  const h = harness(skew)
  const hub = new GuestHub()
  const requests: GuestRunRequest[] = []
  const signals: AbortSignal[] = []
  const settled: string[] = []
  const deleted: (string | undefined)[][] = []
  const kept: string[] = []
  const keptFrom: (string | undefined)[] = []
  const runner = new GuestRunner({
    repo: h.repo,
    hub,
    baseUrl: '/api/v1',
    tab: 'tab',
    onSettled: (id) => settled.push(id),
    onDeleted: (id, from) => deleted.push([id, from]),
    onKept: (id, from) => {
      kept.push(id)
      keptFrom.push(from)
    },
    fetch: async (input, init) => {
      const url = String(input)
      const body = init?.body ? (JSON.parse(String(init.body)) as GuestRunRequest) : ({} as GuestRunRequest)
      if (url.endsWith('/guest/runs')) {
        requests.push(body)
        if (init?.signal) signals.push(init.signal)
      }
      return respond(body, url)
    },
  })
  const seen: ConversationEvent[] = []
  return { h, hub, runner, requests, signals, seen, settled, deleted, kept, keptFrom, listen: (id: string) => hub.subscribe(id, (e) => seen.push(e)) }
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

  it('passes a content moderation refusal on with what the server said, and keeps an edited question', async () => {
    let refuse = false
    const t = setup((request) =>
      refuse
        ? Response.json(
            { code: 'content_blocked', message: 'no', details: { categories: ['violence/graphic'] } },
            { status: 422 },
          )
        : sse(answered(request.conversationId, request)),
    )
    const c = await t.h.conversation()
    await t.runner.start(c, { action: 'send', content: 'one', attachmentIds: [], id: 'in-1' }, {})
    await t.runner.settled(c)
    refuse = true
    const send = t.runner.start(c, { action: 'send', content: 'two', attachmentIds: [], id: 'in-2' }, {})
    await expect(send).rejects.toMatchObject({ status: 422, code: 'content_blocked', details: { categories: ['violence/graphic'] } })
    // The page reads it as the server's own refusal.
    await send.catch((error: { toResponse: () => Response }) => {
      const response = error.toResponse()
      return response.json().then((body) => expect(body.details).toMatchObject({ categories: ['violence/graphic'] }))
    })
    // Refused edit: the question and its answer stay, and nothing is kept for the edit.
    await expect(t.runner.start(c, { action: 'edit', content: 'x', messageId: 'in-1', id: 'in-3' }, {})).rejects.toMatchObject({
      code: 'content_blocked',
    })
    // Only the refused send was kept, as a message of its own.
    expect((await t.h.repo.listMessages(c, {})).items.map((m) => `${m.role}:${m.content}:${m.status}`)).toEqual([
      'user:one:complete',
      'assistant:Hello:complete',
      'user:two:blocked',
    ])
  })

  describe('a send the server refused for its content', () => {
    const refused = () =>
      Response.json({ code: 'content_blocked', message: 'no', details: { categories: ['violence/graphic'], strikesLeft: 2, windowHours: 24 } }, { status: 422 })

    it('is kept as a blocked message at the end, announced, and answered with its id', async () => {
      let refuse = false
      const t = setup((request) => (refuse ? refused() : sse(answered(request.conversationId, request))))
      const c = await t.h.conversation()
      await t.runner.start(c, { action: 'send', content: 'one', attachmentIds: [], id: 'in-1' }, {})
      await t.runner.settled(c)
      t.listen(c)
      refuse = true

      const send = t.runner.start(c, { action: 'send', content: 'BAD', attachmentIds: [], id: 'in-2' }, {})
      const error = await send.catch((e: { toResponse: () => Response }) => e)
      const body = await (error as { toResponse: () => Response }).toResponse().json()
      const stored = (await t.h.repo.data(c)).messages.find((m) => m.status === 'blocked')!
      // The same refusal, with the id of what was kept; the server's own details as they were.
      expect(body).toMatchObject({ code: 'content_blocked', details: { categories: ['violence/graphic'], strikesLeft: 2, windowHours: 24, messageId: stored.id } })
      expect(stored).toMatchObject({ role: 'user', content: 'BAD', parts: [{ type: 'text', text: 'BAD' }] })
      expect(stored.attachments).toBeUndefined()
      // Nothing else of it: no run for it.
      expect((await t.h.repo.data(c)).runs.map((r) => r.id)).toEqual(['run-1'])
      // After the newest row, so that it sorts last.
      const rows = (await t.h.repo.listMessages(c, {})).items
      expect(rows.at(-1)!.id).toBe(stored.id)
      // Other pages of this one and other tabs are told, with nothing to drop.
      expect(t.seen).toEqual([expect.objectContaining({ type: 'batch', events: [{ type: 'transcript_changed' }] })])
      expect(t.kept).toEqual([c])
    })

    it('is never sent to the server afterwards, as a question or as history', async () => {
      let refuse = false
      const t = setup((request) => (refuse ? refused() : sse(answered(request.conversationId, request, { runId: `run-${t.requests.length}` }))))
      const c = await t.h.conversation()
      await t.runner.start(c, { action: 'send', content: 'one', attachmentIds: [], id: 'in-1' }, {})
      await t.runner.settled(c)
      refuse = true
      await t.runner.start(c, { action: 'send', content: 'BAD', attachmentIds: [], id: 'in-2' }, {}).catch(() => undefined)
      refuse = false

      // The conversation goes on by rewording it: the words take its place.
      const blocked = (await t.h.repo.data(c)).messages.find((m) => m.status === 'blocked')!
      await t.runner.start(c, { action: 'edit', content: 'two', messageId: blocked.id, id: 'in-3' }, {})
      await t.runner.settled(c)
      expect(t.requests.at(-1)!.messages.map((m) => m.content)).toEqual(['one', 'Hello'])
      expect(JSON.stringify(t.requests.at(-1))).not.toContain('BAD')
      expect((await t.h.repo.listMessages(c, {})).items.map((m) => m.content)).toEqual(['one', 'Hello', 'two', 'Hello'])
    })

    // The conversation stops at a refused message, as the server's does: only its
    // rewording goes on. The guest's server holds no rows, so it is the browser's rule.
    it('stops the conversation: a send, a regeneration and an answer are refused, and nothing reaches the server', async () => {
      let refuse = false
      const t = setup((request) => (refuse ? refused() : sse(answered(request.conversationId, request))))
      const c = await t.h.conversation()
      await t.runner.start(c, { action: 'send', content: 'one', attachmentIds: [], id: 'in-1' }, {})
      await t.runner.settled(c)
      refuse = true
      await t.runner.start(c, { action: 'send', content: 'BAD', attachmentIds: [], id: 'in-2' }, {}).catch(() => undefined)
      refuse = false
      const before = t.requests.length
      const rows = (await t.h.repo.data(c)).messages.length

      const goingOn: RunAction[] = [
        { action: 'send', content: 'fine', attachmentIds: [], id: 'in-3' },
        { action: 'regenerate' },
        { action: 'answer', callId: 'call', answers: [] },
      ]
      for (const what of goingOn) {
        await expect(t.runner.start(c, what, {})).rejects.toMatchObject({ status: 409, code: 'revise_refused' })
      }
      expect(t.requests.length).toBe(before)
      expect((await t.h.repo.data(c)).messages).toHaveLength(rows)

      // An edit of an earlier question is a way on too: it deletes the refused message with the rest.
      await t.runner.start(c, { action: 'edit', content: 'one again', messageId: 'in-1', id: 'in-4' }, {})
      await t.runner.settled(c)
      expect((await t.h.repo.listMessages(c, {})).items.map((m) => `${m.content}:${m.status}`)).toEqual(['one again:complete', 'Hello:complete'])
    })

    // The page's clock against the script's server, whose rows are at 2026-10-11T00:00:xx.
    it.each([
      ['five minutes ahead', Date.parse('2026-10-11T00:05:00Z') - Date.parse('2026-10-10T10:00:00Z')],
      ['five minutes behind', Date.parse('2026-10-10T23:55:00Z') - Date.parse('2026-10-10T10:00:00Z')],
    ])('keeps its place among the server\'s rows when the page\'s clock is %s', async (_name, skew) => {
      for (const told of [true, false]) {
        let refuse = false
        const t = setup(
          (request) =>
            refuse
              ? Response.json({ code: 'content_blocked', message: 'no', details: { categories: ['violence/graphic'], ...(told ? { at: iso(8) } : {}) } }, { status: 422 })
              : sse(answered(request.conversationId, request, { runId: `run-${t.requests.length}` })),
          skew,
        )
        const c = await t.h.conversation()
        await t.runner.start(c, { action: 'send', content: 'A', attachmentIds: [], id: 'in-1' }, {})
        await t.runner.settled(c)
        refuse = true
        await t.runner.start(c, { action: 'send', content: 'BAD', attachmentIds: [], id: 'in-2' }, {}).catch(() => undefined)
        const texts = async () => (await t.h.repo.listMessages(c, {})).items.map((m) => m.content)
        // A, its answer, the refused message: the order they were said in.
        expect(await texts()).toEqual(['A', 'Hello', 'BAD'])

        // Reworded and refused again, the new words take its place - still last.
        const blocked = (await t.h.repo.data(c)).messages.find((m) => m.status === 'blocked')!
        await t.runner.start(c, { action: 'edit', content: 'BAD2', messageId: blocked.id, id: 'in-3' }, {}).catch(() => undefined)
        expect(await texts()).toEqual(['A', 'Hello', 'BAD2'])

        // Reworded to something that passes: it replaces the refused message, and
        // the request that asks for the new wording carries none of that as context.
        refuse = false
        const again = (await t.h.repo.data(c)).messages.find((m) => m.status === 'blocked')!
        await t.runner.start(c, { action: 'edit', content: 'OK', messageId: again.id, id: 'in-4' }, {})
        await t.runner.settled(c)
        expect(t.requests.at(-1)!.messages.map((m) => m.content)).toEqual(['A', 'Hello'])
        expect(JSON.stringify(t.requests.at(-1))).not.toContain('BAD')
        expect(await texts()).toEqual(['A', 'Hello', 'OK', 'Hello'])
      }
    })

    it('is kept for a conversation that has nothing else, and words refused again take its place', async () => {
      const t = setup(refused)
      const c = await t.h.conversation()
      await expect(t.runner.start(c, { action: 'send', content: 'BAD', attachmentIds: [], id: 'in-1' }, {})).rejects.toMatchObject({ code: 'content_blocked' })
      expect((await t.h.repo.data(c)).messages.map((m) => [m.content, m.status])).toEqual([['BAD', 'blocked']])
      // An edit of the refused message is refused again: what was last tried is the refused message.
      const blockedId = (await t.h.repo.data(c)).messages[0]!.id
      const error = await t.runner.start(c, { action: 'edit', content: 'BAD2', messageId: blockedId, id: 'in-2' }, {}).catch((e: { toResponse: () => Response }) => e)
      const body = await (error as { toResponse: () => Response }).toResponse().json()
      const rows = (await t.h.repo.data(c)).messages
      expect(rows.map((m) => [m.content, m.status])).toEqual([['BAD2', 'blocked']])
      // The refusal names the row that stands now, and the other pages are told which went.
      expect(body).toMatchObject({ code: 'content_blocked', details: { messageId: rows[0]!.id } })
      expect(rows[0]!.id).not.toBe(blockedId)
      expect(t.kept).toEqual([c, c])
      expect(t.keptFrom).toEqual([undefined, blockedId])
      // The request of that edit carried nothing before it, and not the refused message.
      expect(t.requests.at(-1)!.messages).toEqual([])
    })

    it('leaves an ordinary question alone when its reworded form is refused, and a refused message with rows after it', async () => {
      const t = setup(refused)
      const c = await t.h.conversation()
      await t.h.turn(c, { ask: 'one', answer: 'a1' })
      const data = await t.h.repo.data(c)
      const question = data.messages.find((m) => m.role === 'user')!
      await expect(t.runner.start(c, { action: 'edit', content: 'x', messageId: question.id, id: 'in-2' }, {})).rejects.toMatchObject({ code: 'content_blocked' })
      expect((await t.h.repo.data(c)).messages.map((m) => m.status)).toEqual(['complete', 'complete'])

      // Old data: a refused message with a row after it is not replaced by a refused edit.
      const blocked = await t.h.repo.keepBlocked(c, 'BAD', t.h.tick())
      await t.h.repo.messageAppended(c, {
        id: 'after', conversationId: c, role: 'user', content: 'after it', createdAt: iso(70), status: 'complete', parts: [{ type: 'text', text: 'after it' }],
      })
      await expect(t.runner.start(c, { action: 'edit', content: 'BAD2', messageId: blocked!.id, id: 'in-3' }, {})).rejects.toMatchObject({ code: 'content_blocked' })
      expect((await t.h.repo.data(c)).messages.map((m) => m.content)).toEqual(['one', 'a1', 'BAD', 'after it'])
    })

    it('takes the composer\'s files with the reworded message, and leaves them when refused again', async () => {
      let refuse = true
      const t = setup((request) => (refuse ? refused() : sse(answered(request.conversationId, request))))
      const c = await t.h.conversation()
      const png = (id: string) => ({ id, conversationId: '', blob: new Blob(['x']), filename: `${id}.png`, contentType: 'image/png', size: 1 })
      await t.h.repo.putFile(png('f1'))
      await t.runner.start(c, { action: 'send', content: 'BAD', attachmentIds: ['f1'], id: 'in-1' }, {}).catch(() => undefined)
      const blocked = (await t.h.repo.data(c)).messages[0]!
      expect(blocked.attachments).toBeUndefined()

      // Refused again with the file: it is still the composer's.
      await t.runner.start(c, { action: 'edit', content: 'BAD2', messageId: blocked.id, id: 'in-2', attachmentIds: ['f1'] }, {}).catch(() => undefined)
      expect(await t.h.repo.file('f1')).toBeDefined()
      expect((await t.h.repo.data(c)).messages.every((m) => !m.attachments)).toBe(true)

      refuse = false
      const again = (await t.h.repo.data(c)).messages[0]!
      await t.runner.start(c, { action: 'edit', content: 'what is this', messageId: again.id, id: 'in-3', attachmentIds: ['f1'] }, {})
      await t.runner.settled(c)
      const sent = t.requests.at(-1)!
      expect(sent.input!.attachments!.map((f) => [f.id, f.data !== undefined])).toEqual([['f1', true]])
      expect((await t.h.repo.data(c)).messages.find((m) => m.id === 'in-3')!.attachments!.map((f) => f.id)).toEqual(['f1'])

      // Any other question keeps its own and takes none.
      await t.h.repo.putFile(png('f2'))
      await expect(
        t.runner.start(c, { action: 'edit', content: 'again', messageId: 'in-3', id: 'in-4', attachmentIds: ['f2'] }, {}),
      ).rejects.toMatchObject({ status: 400, code: 'attachments_not_editable' })
    })

    it('is not kept when the refusal is any other (a ban, a limit, an outage)', async () => {
      const t = setup(() => Response.json({ code: 'moderation_unavailable', message: 'x' }, { status: 503 }))
      const c = await t.h.conversation()
      await expect(t.runner.start(c, { action: 'send', content: 'hi', attachmentIds: [], id: 'in-1' }, {})).rejects.toMatchObject({ status: 503 })
      expect((await t.h.repo.data(c)).messages).toEqual([])
      expect(t.kept).toEqual([])
    })

    it('is not kept while a run is going, as the server keeps nothing then', async () => {
      const t = setup(refused)
      const c = await t.h.conversation()
      await t.h.turn(c, { ask: 'q', status: 'running' })
      expect(await t.h.repo.keepBlocked(c, 'BAD')).toBeNull()
      expect((await t.h.repo.data(c)).messages.map((m) => m.status)).not.toContain('blocked')
    })
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
    // The page asks again the moment it is told the first run finished, which is
    // before the run's closing frame (the memories, the title) has been read.
    const finished = new Promise<void>((resolve) =>
      t.hub.subscribe(c, (e) => {
        if (e.type === 'batch' && e.events.some((x) => x.type === 'run_finished')) resolve()
      }),
    )
    await t.runner.start(c, { action: 'send', content: 'one', attachmentIds: [], id: 'in-1' }, {})
    await finished
    await t.runner.start(c, { action: 'send', content: 'two', attachmentIds: [], id: 'in-2' }, {})
    const second = t.requests[1]!
    expect(second.memories).toEqual([{ id: 'm1', content: 'Likes tea.', source: 'model' }])
    expect(second.memorySnapshot).toEqual({ about: 'hi' })
    expect(second.titled).toBe(true)
    expect(second.messages.map((m) => m.id)).toEqual(['in-1', 'answer-1'])
    expect(second.messages[1]!.providerState).toBe('cHM=')
    await t.runner.settled(c)
  })

  /** A server that streams the start and then holds the answer until released. */
  const holding = () => {
    let release = () => {}
    const gate = new Promise<void>((resolve) => (release = resolve))
    return {
      release: () => release(),
      respond: (request: GuestRunRequest) => {
        const frames = answered(request.conversationId, request)
        return new Response(
          new ReadableStream({
            async start(controller) {
              const encoder = new TextEncoder()
              const send = (f: unknown) => controller.enqueue(encoder.encode(`data: ${JSON.stringify(f)}\n\n`))
              send(frames[0])
              await gate
              frames.slice(1).forEach(send)
              controller.close()
            },
          }),
        )
      },
    }
  }

  it('refuses a question asked while the conversation is answering, instead of queuing it', async () => {
    const server = holding()
    const t = setup(server.respond)
    const c = await t.h.conversation()
    await t.runner.start(c, { action: 'send', content: 'one', attachmentIds: [], id: 'in-1' }, {})
    expect(t.runner.isRunning(c)).toBe(true)

    await expect(t.runner.start(c, { action: 'send', content: 'two', attachmentIds: [], id: 'in-2' }, {})).rejects.toMatchObject({
      status: 409,
      code: 'conversation_busy',
    })
    // Nothing went to the server, nothing was written.
    expect(t.requests).toHaveLength(1)
    expect((await t.h.repo.data(c)).messages.map((m) => m.id)).toEqual(['in-1'])

    server.release()
    await t.runner.settled(c)
    // And once it is over, asking works again.
    await t.runner.start(c, { action: 'send', content: 'two', attachmentIds: [], id: 'in-2' }, {})
    expect(t.requests).toHaveLength(2)
    await t.runner.settled(c)
  })

  it('refuses a second start made while the first is still being taken', async () => {
    const t = setup((request) => sse(answered(request.conversationId, request)))
    const c = await t.h.conversation()
    const [first, second] = await Promise.allSettled([
      t.runner.start(c, { action: 'send', content: 'one', attachmentIds: [], id: 'in-1' }, {}),
      t.runner.start(c, { action: 'send', content: 'two', attachmentIds: [], id: 'in-2' }, {}),
    ])
    expect(first.status).toBe('fulfilled')
    expect(second).toMatchObject({ status: 'rejected', reason: { code: 'conversation_busy' } })
    expect(t.requests).toHaveLength(1)
    await t.runner.settled(c)
  })

  it('cancels the server\'s run when the browser could not keep the start', async () => {
    const server = holding()
    const t = setup(server.respond)
    const c = await t.h.conversation()
    vi.spyOn(t.h.repo, 'runStarted').mockRejectedValue(new Error('storage: quota'))

    await expect(t.runner.start(c, { action: 'send', content: 'one', attachmentIds: [], id: 'in-1' }, {})).rejects.toThrow('quota')
    expect(t.signals[0]!.aborted).toBe(true)
    expect(t.runner.isRunning(c)).toBe(false)
    server.release()
  })

  it('still tells the page the run is over when the storage fails while it is read', async () => {
    const t = setup((request) => sse(answered(request.conversationId, request)))
    const c = await t.h.conversation()
    t.listen(c)
    await t.runner.start(c, { action: 'send', content: 'hi', attachmentIds: [], id: 'in-1' }, {})
    // From here on nothing can be written: the first answer row cannot be kept,
    // and neither can the failure.
    vi.spyOn(t.h.repo, 'messageAppended').mockRejectedValue(new Error('storage: gone'))
    vi.spyOn(t.h.repo, 'runInterrupted').mockRejectedValue(new Error('storage: gone'))
    await t.runner.settled(c)

    const last = t.seen.at(-1) as BatchEvent
    expect(last.events).toHaveLength(1)
    const finished = last.events[0]!
    expect(finished).toMatchObject({ type: 'run_finished', run: { id: 'run-1', status: 'failed', errorCode: 'connection_lost' } })
    // Newer than anything told before it, or the page would take it for old news.
    const told = t.seen.slice(0, -1).flatMap((e) => (e.type === 'batch' ? [e.version] : []))
    expect(last.version).toBeGreaterThan(Math.max(...told))
    expect(t.hub.live(c).run?.run.status).toBe('failed')
    expect(t.signals[0]!.aborted).toBe(true)
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
    // The edited question is named, so that other pages drop it at once.
    expect(t.deleted).toEqual([[c, 'in-1']])

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

  it('removes a refused message with the answer when the last question is answered again', async () => {
    const t = serving()
    const c = await t.h.conversation()
    await t.runner.start(c, { action: 'send', content: 'one', attachmentIds: [], id: 'in-1' }, {})
    await t.runner.settled(c)
    await t.h.repo.keepBlocked(c, 'BAD')
    // Old data: a reply written after the refused message (the conversation stops
    // at one now, but did not once).
    const reply: Message = {
      id: 'stray', conversationId: c, role: 'assistant', content: 'stray', status: 'complete', createdAt: iso(60),
      stopReason: 'stop', parts: [{ type: 'text', text: 'stray' }],
    }
    await t.h.repo.messageAppended(c, reply)
    expect(await texts(t, c)).toEqual(['user:one', 'assistant:Hello', 'user:BAD', 'assistant:stray'])

    await t.runner.start(c, { action: 'regenerate' }, {})
    await t.runner.settled(c)
    expect(t.requests.at(-1)!.messages.map((m) => m.id)).toEqual(['in-1'])
    expect(await texts(t, c)).toEqual(['user:one', 'assistant:Hello'])
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

  it('never carries a message moderation refused, and answers the question before it', async () => {
    const h = harness()
    const c = await h.conversation()
    await h.turn(c, { ask: 'q1', answer: 'a1' })
    const q2 = await h.turn(c, { ask: 'q2', answer: 'a2' })
    const kept = await h.repo.keepBlocked(c, 'BAD')
    expect(kept?.status).toBe('blocked')

    const send = await buildRunRequest(await base(h, c), { action: 'send', content: 'q3', id: 'new', attachmentIds: [] })
    expect(send.request.messages.map((m) => m.content)).toEqual(['q1', 'a1', 'q2', 'a2'])

    // Regenerating answers the last question asked, not the refused message after it.
    const regenerate = await buildRunRequest(await base(h, c), { action: 'regenerate' })
    expect(regenerate.request.messages.map((m) => m.content)).toEqual(['q1', 'a1', 'q2'])

    // Editing a question before it asks from what came before the question.
    const edit = await buildRunRequest(await base(h, c), { action: 'edit', content: 'q2 again', messageId: q2.input.id, id: 'e' })
    expect(edit.request.messages.map((m) => m.content)).toEqual(['q1', 'a1'])
    // Editing the refused message itself is an edit like any other: it is asked from what came before it.
    const editRefused = await buildRunRequest(await base(h, c), { action: 'edit', content: 'ok now', messageId: kept!.id, id: 'e2' })
    expect(editRefused.request.messages.map((m) => m.content)).toEqual(['q1', 'a1', 'q2', 'a2'])
    expect(editRefused.input?.replaces).toBe(kept!.id)
    // A conversation of nothing else has nothing to regenerate.
    const alone = await h.conversation()
    await h.repo.keepBlocked(alone, 'BAD')
    await expect(buildRunRequest(await base(h, alone), { action: 'regenerate' })).rejects.toMatchObject({ code: 'nothing_to_answer' })
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
