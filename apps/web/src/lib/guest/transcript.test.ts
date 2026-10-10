import { describe, expect, it } from 'vitest'

import { GuestError } from './errors'
import { harness, type Harness } from './testing'
import { deletedBy, pageOf, timeKey } from './transcript'

// The rules of a conversation (AGENTS.md, "对话是一条线"), as the guest's
// browser applies them: the transcript is every row in the order it was
// written; editing a question deletes it and everything after it and the new
// wording goes on the end; regenerating deletes the old answer.

const texts = async (h: Harness, id: string) => (await h.repo.listMessages(id, {})).items.map((m) => `${m.role}:${m.content}`)

describe('the transcript', () => {
  it('is every row of the conversation, each question with its answer', async () => {
    const h = harness()
    const c = await h.conversation()
    await h.turn(c, { ask: 'q1', answer: 'a1' })
    await h.turn(c, { ask: 'q2', answer: 'a2', tools: true })
    const page = await h.repo.listMessages(c, {})
    expect(page.items.map((m) => m.role)).toEqual(['user', 'assistant', 'user', 'assistant', 'tool', 'assistant'])
    expect(page.olderCursor).toBeNull()
    // What a provider wants back is the runner's to hold, never the page's.
    expect(page.items.every((m) => !('providerState' in m))).toBe(true)
    // No pager: a message has no other versions to offer.
    expect(page.items.every((m) => !('versions' in m) && !('branches' in m))).toBe(true)
  })

  it('puts the new wording of an edited question on the end, without the question or what followed it', async () => {
    const h = harness()
    const c = await h.conversation()
    await h.turn(c, { ask: 'q1', answer: 'a1' })
    const second = await h.turn(c, { ask: 'q2', answer: 'a2' })
    await h.turn(c, { ask: 'q3', answer: 'a3' })

    await h.turn(c, { ask: 'q2 again', answer: 'a2 again', replaces: second.input.id })
    expect(await texts(h, c)).toEqual(['user:q1', 'assistant:a1', 'user:q2 again', 'assistant:a2 again'])
    const data = await h.repo.data(c)
    // The questions that went are gone from storage, with their runs.
    expect(data.messages.map((m) => m.content)).not.toContain('q3')
    expect(data.runs.filter((r) => r.inputId === second.input.id)).toEqual([])
    expect(data.runs).toHaveLength(2)
  })

  it('empties the conversation of everything but the new wording when the first question is edited', async () => {
    const h = harness()
    const c = await h.conversation()
    const first = await h.turn(c, { ask: 'q1', answer: 'a1' })
    await h.turn(c, { ask: 'q2', answer: 'a2' })
    await h.turn(c, { ask: 'q1 again', answer: 'b1', replaces: first.input.id })
    expect(await texts(h, c)).toEqual(['user:q1 again', 'assistant:b1'])
  })

  it('keeps the files an edited question carried, and deletes those of the questions that went with it', async () => {
    const h = harness()
    const c = await h.conversation()
    const blob = (id: string) => ({ id, conversationId: c, blob: new Blob(['x']), filename: `${id}.png`, contentType: 'image/png', size: 1 })
    const kept = blob('kept')
    const dropped = blob('dropped')
    await h.repo.storage.putBlob(kept)
    await h.repo.storage.putBlob(dropped)
    const file = ({ id, filename, contentType, size }: ReturnType<typeof blob>) => ({ id, filename, contentType, size })
    const q1 = await h.turn(c, { ask: 'q1', answer: 'a1' })
    const q2 = await h.turn(c, { ask: 'q2', answer: 'a2' })
    const q3 = await h.turn(c, { ask: 'q3', answer: 'a3' })
    await h.repo.storage.putMessages([
      { ...q2.input, attachments: [file(kept)] },
      { ...q3.input, attachments: [file(dropped)] },
    ])

    // q2 is edited and carries its file over, as the request builder arranges.
    await h.repo.runStarted(
      c,
      {
        run: { id: 'run-edit', conversationId: c, status: 'running', model: 'm', version: 1, createdAt: h.tick() },
        input: { ...q2.input, id: 'q2b', content: 'q2b', createdAt: h.tick() },
        action: 'send',
        replaces: q2.input.id,
        attachments: [kept],
        choice: {},
      },
      'tab',
    )
    expect(await h.repo.storage.blob('kept')).toBeDefined()
    expect(await h.repo.storage.blob('dropped')).toBeUndefined()
    const data = await h.repo.data(c)
    expect(data.messages.find((m) => m.id === 'q2b')!.attachments?.map((f) => f.id)).toEqual(['kept'])
    expect(data.messages.map((m) => m.id)).toContain(q1.input.id)
  })
})

describe('answers', () => {
  it('deletes the earlier answer when regenerating', async () => {
    const h = harness()
    const c = await h.conversation()
    const q = await h.turn(c, { ask: 'q', answer: 'first' })
    const again = await h.turn(c, { action: 'regenerate', input: q.input.id, answer: 'second' })

    expect(await texts(h, c)).toEqual(['user:q', 'assistant:second'])
    const data = await h.repo.data(c)
    expect(data.runs.map((r) => r.id)).toEqual([again.run.id])
    expect(data.messages).toHaveLength(2)
  })

  it('deletes a run that went on after a question together with the one that asked', async () => {
    const h = harness()
    const c = await h.conversation()
    const q = await h.turn(c, { ask: 'q', answer: 'which one?', status: 'awaiting_input' })
    const resumed = await h.turn(c, { action: 'answer', input: q.input.id, continues: q.run.id, answer: 'done' })

    expect(await texts(h, c)).toEqual(['user:q', 'assistant:which one?', 'assistant:done'])
    expect(resumed.run.continues).toBe(q.run.id)
    // Regenerating deletes both halves.
    const fresh = await h.turn(c, { action: 'regenerate', input: q.input.id, answer: 'fresh' })
    expect(await texts(h, c)).toEqual(['user:q', 'assistant:fresh'])
    expect((await h.repo.data(c)).runs.map((r) => r.id)).toEqual([fresh.run.id])
  })

  it('does not touch the turns before the one regenerated', async () => {
    const h = harness()
    const c = await h.conversation()
    await h.turn(c, { ask: 'q1', answer: 'a1' })
    const q2 = await h.turn(c, { ask: 'q2', answer: 'a2' })
    await h.turn(c, { action: 'regenerate', input: q2.input.id, answer: 'a2 again' })
    expect(await texts(h, c)).toEqual(['user:q1', 'assistant:a1', 'user:q2', 'assistant:a2 again'])
  })
})

describe('deletedBy', () => {
  const row = (id: string, role: 'user' | 'assistant', n: number) =>
    ({ id, role, createdAt: new Date(1_700_000_000_000 + n * 1000).toISOString() }) as never
  const rows = [row('q1', 'user', 1), row('a1', 'assistant', 2), row('q2', 'user', 3), row('a2', 'assistant', 4)]

  it('takes the message and everything after it, for an edit', () => {
    expect(deletedBy(rows, { from: 'q2' }).map((m: { id: string }) => m.id)).toEqual(['q2', 'a2'])
    expect(deletedBy(rows, { from: 'q1' })).toHaveLength(4)
  })

  it('takes the answer after the question, for a regeneration', () => {
    expect(deletedBy(rows, { after: 'q2' }).map((m: { id: string }) => m.id)).toEqual(['a2'])
  })

  it('takes nothing for a message that is not there', () => {
    expect(deletedBy(rows, { from: 'nope' })).toEqual([])
  })
})

describe('paging', () => {
  it('pages from the end, oldest first within a page, with a cursor to the older rows', async () => {
    const h = harness()
    const c = await h.conversation()
    for (let i = 1; i <= 5; i++) await h.turn(c, { ask: `q${i}`, answer: `a${i}` })

    const newest = await h.repo.listMessages(c, { limit: 4 })
    expect(newest.items.map((m) => m.content)).toEqual(['q4', 'a4', 'q5', 'a5'])
    expect(newest.olderCursor).toBeTruthy()
    const older = await h.repo.listMessages(c, { limit: 4, before: newest.olderCursor! })
    expect(older.items.map((m) => m.content)).toEqual(['q2', 'a2', 'q3', 'a3'])
    const oldest = await h.repo.listMessages(c, { limit: 4, before: older.olderCursor! })
    expect(oldest.items.map((m) => m.content)).toEqual(['q1', 'a1'])
    expect(oldest.olderCursor).toBeNull()
  })

  it('orders rows by the instant, microseconds included', () => {
    const rows = [
      { id: 'b', createdAt: '2026-10-10T18:53:57.506430+08:00' },
      { id: 'a', createdAt: '2026-10-10T18:53:57.506429+08:00' },
      { id: 'c', createdAt: '2026-10-10T10:53:57.5064301Z' },
    ]
    expect(pageOf(rows, undefined, 10).items.map((r) => r.id)).toEqual(['a', 'b', 'c'])
    expect(timeKey('2026-10-10T18:53:57.5+08:00')).toBe('2026-10-10T10:53:57.500000000')
  })
})

describe('errors', () => {
  it('speaks the server\'s language', async () => {
    const h = harness()
    await expect(h.repo.getConversation('missing')).rejects.toBeInstanceOf(GuestError)
    await expect(h.repo.getConversation('missing')).rejects.toMatchObject({ status: 404, code: 'not_found' })
  })
})
