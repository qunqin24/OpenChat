import type { Memory, MemoryEvent } from '@openchat/api-client'
import { describe, expect, it } from 'vitest'

import { MAX_CONTENT, MAX_MEMORIES } from './memory'
import { excerpt, RUN_STALE_MS, TEMPORARY_LIFETIME_MS } from './repo'
import { harness } from './testing'

describe('the conversation list', () => {
  it('lists newest activity first, pages by cursor, and leaves temporary chats out', async () => {
    const h = harness()
    const ids: string[] = []
    for (let i = 0; i < 5; i++) {
      h.tick(60_000)
      ids.push(await h.conversation({ title: `chat ${i}` }))
    }
    await h.conversation({ temporary: true })

    const first = await h.repo.listConversations({ limit: 2 })
    expect(first.items.map((c) => c.title)).toEqual(['chat 4', 'chat 3'])
    expect(first.nextCursor).toBeTruthy()
    const second = await h.repo.listConversations({ limit: 2, cursor: first.nextCursor! })
    expect(second.items.map((c) => c.title)).toEqual(['chat 2', 'chat 1'])
    const last = await h.repo.listConversations({ limit: 2, cursor: second.nextCursor! })
    expect(last.items.map((c) => c.title)).toEqual(['chat 0'])
    expect(last.nextCursor).toBeNull()
    expect(ids).toHaveLength(5)

    // Activity moves a chat to the top.
    h.tick(60_000)
    await h.repo.updateConversation(ids[0]!, { title: 'chat 0, renamed' })
    expect((await h.repo.listConversations({})).items[0]!.title).toBe('chat 0, renamed')
  })

  it('searches titles and everything said, with the passage around the hit', async () => {
    const h = harness()
    const a = await h.conversation({ title: 'Trip to Lisbon' })
    const b = await h.conversation({ title: 'Untitled' })
    const q = await h.turn(b, { ask: 'how do I cook a risotto with mushrooms', answer: 'Stir it slowly.' })

    const byTitle = await h.repo.listConversations({ search: 'lisbon' })
    expect(byTitle.items.map((c) => c.id)).toEqual([a])
    expect(byTitle.items[0]!.match).toBeUndefined()

    const byWords = await h.repo.listConversations({ search: 'RISOTTO' })
    expect(byWords.items.map((c) => c.id)).toEqual([b])
    expect(byWords.items[0]!.match).toMatchObject({ messageId: q.input.id, role: 'user' })
    expect(byWords.items[0]!.match!.excerpt).toContain('risotto')

    // What was edited away was deleted, so it is not found any more.
    await h.turn(b, { ask: 'something else', answer: 'different', replaces: q.input.id })
    expect((await h.repo.listConversations({ search: 'risotto' })).items).toEqual([])
    expect((await h.repo.listConversations({ search: 'different' })).items.map((c) => c.id)).toEqual([b])
  })

  it('cuts an excerpt to the words around the first hit', () => {
    const text = `${'word '.repeat(30)}needle ${'tail '.repeat(40)}`
    const cut = excerpt(text, 'NEEDLE')
    expect(cut.startsWith('…')).toBe(true)
    expect(cut.endsWith('…')).toBe(true)
    expect(cut).toContain('needle')
    expect(excerpt('short text', 'text')).toBe('short text')
  })

  it('deletes temporary chats a day after they were made', async () => {
    const h = harness()
    const temp = await h.conversation({ temporary: true })
    expect((await h.repo.getConversation(temp)).temporary).toBe(true)
    h.clock.now += TEMPORARY_LIFETIME_MS + 120_000
    await expect(h.repo.getConversation(temp)).rejects.toMatchObject({ status: 404 })
  })

  it('remembers what the composer chose, and refuses nonsense', async () => {
    const h = harness()
    const c = await h.conversation()
    const updated = await h.repo.updateConversation(c, { model: 'm1', reasoningEffort: 'high', webSearch: false })
    expect(updated).toMatchObject({ model: 'm1', reasoningEffort: 'high', webSearch: false })
    await expect(h.repo.updateConversation(c, {})).rejects.toMatchObject({ code: 'nothing_to_update' })
    await expect(h.repo.updateConversation(c, { title: '  ' })).rejects.toMatchObject({ code: 'invalid_title' })
    await expect(h.repo.createConversation({ reasoningEffort: 'bogus' as never })).rejects.toMatchObject({ code: 'invalid_reasoning_effort' })
  })

  it('deletes a conversation with everything of it, and all of them on request', async () => {
    const h = harness()
    const a = await h.conversation()
    const b = await h.conversation({ temporary: true })
    await h.turn(a, { ask: 'q', answer: 'a' })
    await h.repo.deleteConversation(a)
    expect(await h.repo.storage.messages(a)).toEqual([])
    expect(await h.repo.storage.runs(a)).toEqual([])
    await expect(h.repo.deleteConversation(a)).rejects.toMatchObject({ status: 404 })
    expect(await h.repo.deleteAll()).toBe(1)
    await expect(h.repo.getConversation(b)).rejects.toMatchObject({ status: 404 })
  })
})

describe('runs', () => {
  it('measures the context from the latest answered request', async () => {
    const h = harness()
    const c = await h.conversation()
    expect(await h.repo.context(c)).toEqual({})
    await h.turn(c, { ask: 'q1', answer: 'a1', usage: { inputTokens: 100, outputTokens: 20 } })
    await h.turn(c, { ask: 'q2', answer: 'a2', usage: { inputTokens: 10, outputTokens: 5, cacheReadTokens: 200, cacheWriteTokens: 3 } })
    expect(await h.repo.context(c)).toEqual({ usedTokens: 218, model: 'model-a' })
  })

  it('knows which conversations are being answered, and gives up on a run whose page went away', async () => {
    const h = harness()
    const c = await h.conversation()
    await h.turn(c, { ask: 'q', status: 'running' })
    expect(await h.repo.running()).toEqual([c])

    h.clock.now += RUN_STALE_MS + 1000
    expect(await h.repo.running()).toEqual([])
    const data = await h.repo.data(c)
    expect(data.runs[0]!.status).toBe('cancelled')
    expect(h.repo.activeRun(data)).toBeUndefined()
  })

  it('turns a lost connection into a failed run with an empty failed reply', async () => {
    const h = harness()
    const c = await h.conversation()
    const turn = await h.turn(c, { ask: 'q', status: 'running' })
    const lost = await h.repo.runInterrupted(c, turn.run.id, 'connection_lost')
    expect(lost?.run).toMatchObject({ status: 'failed', errorCode: 'connection_lost' })
    expect(lost?.message).toMatchObject({ role: 'assistant', stopReason: 'error', errorCode: 'connection_lost', status: 'failed' })
    const page = await h.repo.listMessages(c, {})
    expect(page.items.map((m) => m.role)).toEqual(['user', 'assistant'])
    // Over once: a second call has nothing to do.
    expect(await h.repo.runInterrupted(c, turn.run.id, 'connection_lost')).toBeNull()
  })

  it('keeps what the last frame brings: snapshot, memories, log, title', async () => {
    const h = harness()
    const c = await h.conversation()
    const turn = await h.turn(c, { ask: 'q', answer: 'a' })
    const added = memory('m1', 'Likes jazz.', h.tick())
    const event = memoryEvent('e1', 'm1', 'add', { after: 'Likes jazz.' }, h.tick())
    await h.repo.runEnded(c, { run: turn.run, memorySnapshot: { about: 'x' }, memories: [added], memoryEvents: [event], title: 'About jazz' })

    expect((await h.repo.getConversation(c)).title).toBe('About jazz')
    expect((await h.repo.data(c)).conversation.memorySnapshot).toEqual({ about: 'x' })
    expect((await h.repo.memories()).map((m) => m.content)).toEqual(['Likes jazz.'])
    expect((await h.repo.memoryEvents()).map((e) => e.id)).toEqual(['e1'])

    // A title the guest typed is not overwritten by a generated one.
    await h.repo.updateConversation(c, { title: 'Mine' })
    await h.repo.runEnded(c, { run: turn.run, memorySnapshot: null, memories: [added], memoryEvents: [], title: 'Generated' })
    expect((await h.repo.getConversation(c)).title).toBe('Mine')
  })
})

const memory = (id: string, content: string, at: string): Memory => ({ id, content, source: 'model', createdAt: at, updatedAt: at })
const memoryEvent = (
  id: string,
  memoryId: string,
  op: MemoryEvent['op'],
  over: Partial<MemoryEvent>,
  at: string,
): MemoryEvent => ({ id, memoryId, op, source: 'model', createdAt: at, ...over })

describe('personalization and memory', () => {
  it('reports the administrator\'s limits with the guest\'s own instructions', async () => {
    const h = harness()
    expect(await h.repo.personalization()).toMatchObject({ aboutYou: '', memoryEnabled: true, memoryAllowed: true, memoryLimit: MAX_MEMORIES })
    const next = await h.repo.updatePersonalization({ aboutYou: 'I am Ada', memoryEnabled: false })
    expect(next).toMatchObject({ aboutYou: 'I am Ada', responseStyle: '', memoryEnabled: false })
    await expect(h.repo.updatePersonalization({ responseStyle: 'x'.repeat(1501) })).rejects.toMatchObject({ code: 'instructions_too_long' })
  })

  it('applies the server\'s rules to notes', async () => {
    const h = harness()
    const first = await h.repo.createMemory('  Likes   tea \n and coffee ')
    expect(first.content).toBe('Likes tea and coffee')
    await expect(h.repo.createMemory('likes TEA and coffee')).rejects.toMatchObject({ code: 'memory_duplicate', status: 409 })
    await expect(h.repo.createMemory('   ')).rejects.toMatchObject({ code: 'memory_empty', status: 400 })
    await expect(h.repo.createMemory('x'.repeat(MAX_CONTENT + 1))).rejects.toMatchObject({ code: 'memory_too_long' })
    const other = await h.repo.createMemory('Lives in Lisbon')
    await expect(h.repo.updateMemory(other.id, 'Likes tea and coffee')).rejects.toMatchObject({ code: 'memory_duplicate' })
    expect((await h.repo.updateMemory(other.id, 'Lives in Porto')).content).toBe('Lives in Porto')

    for (let i = 0; i < MAX_MEMORIES - 2; i++) await h.repo.createMemory(`note ${i}`)
    await expect(h.repo.createMemory('one too many')).rejects.toMatchObject({ code: 'memory_full' })

    await h.repo.deleteMemory(first.id)
    await expect(h.repo.deleteMemory(first.id)).rejects.toMatchObject({ status: 404 })
    await h.repo.clearMemories()
    expect(await h.repo.memories()).toEqual([])
  })

  it('undoes what the model did, while the note still reads as it left it', async () => {
    const h = harness()
    const c = await h.conversation()
    const turn = await h.turn(c, { ask: 'q', answer: 'a' })
    const at = h.tick()
    // The model added a note, rewrote another, deleted a third.
    const added = memory('m-add', 'Likes jazz.', at)
    const rewritten = memory('m-upd', 'Lives in Porto.', at)
    await h.repo.runEnded(c, {
      run: turn.run,
      memorySnapshot: null,
      memories: [added, rewritten],
      memoryEvents: [
        memoryEvent('e-add', 'm-add', 'add', { after: 'Likes jazz.' }, at),
        memoryEvent('e-upd', 'm-upd', 'update', { before: 'Lives in Lisbon.', after: 'Lives in Porto.' }, at),
        memoryEvent('e-del', 'm-del', 'delete', { before: 'Has a cat.' }, at),
      ],
    })

    expect((await h.repo.undoMemoryEvent('e-add')).undoneAt).toBeTruthy()
    await expect(h.repo.undoMemoryEvent('e-add')).rejects.toMatchObject({ code: 'already_undone' })
    expect((await h.repo.memories()).map((m) => m.content)).toEqual(['Lives in Porto.'])

    await h.repo.undoMemoryEvent('e-upd')
    expect((await h.repo.memories()).map((m) => m.content)).toEqual(['Lives in Lisbon.'])

    await h.repo.undoMemoryEvent('e-del')
    expect((await h.repo.memories()).map((m) => m.content).sort()).toEqual(['Has a cat.', 'Lives in Lisbon.'])
    await expect(h.repo.undoMemoryEvent('nope')).rejects.toMatchObject({ status: 404 })
  })

  it('lets a person\'s own edit win over an undo, and forgets the log of a note they change', async () => {
    const h = harness()
    const c = await h.conversation()
    const turn = await h.turn(c, { ask: 'q', answer: 'a' })
    const at = h.tick()
    await h.repo.runEnded(c, {
      run: turn.run,
      memorySnapshot: null,
      memories: [memory('m1', 'Likes jazz.', at), memory('m2', 'Has a dog.', at)],
      memoryEvents: [memoryEvent('e1', 'm1', 'add', { after: 'Likes jazz.' }, at), memoryEvent('e2', 'm2', 'add', { after: 'Has a dog.' }, at)],
    })
    await h.repo.updateMemory('m1', 'Likes blues.')
    expect((await h.repo.memoryEvents()).map((e) => e.id)).toEqual(['e2'])
    await h.repo.deleteMemory('m2')
    expect(await h.repo.memoryEvents()).toEqual([])

    // The log keeps thirty days.
    await h.repo.storage.putMemoryEvent(memoryEvent('old', 'm9', 'add', {}, new Date(h.clock.now - 31 * 24 * 3600 * 1000).toISOString()))
    expect(await h.repo.memoryEvents()).toEqual([])
  })
})

describe('the data page', () => {
  it('exports the conversation as text, with file names and without tool rows', async () => {
    const h = harness()
    const c = await h.conversation({ title: 'Export me' })
    await h.turn(c, { ask: 'q1', answer: 'a1', tools: true })
    const doc = await h.repo.export()
    expect(doc).toMatchObject({ version: 1 })
    expect(doc.conversations).toHaveLength(1)
    expect(doc.conversations[0]!.messages.map((m) => `${m.role}:${m.content}`)).toEqual(['user:q1', 'assistant:a1'])
  })
})
