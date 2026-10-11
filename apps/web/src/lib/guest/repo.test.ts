import type { Memory, MemoryEvent, Message, Run } from '@openchat/api-client'
import { describe, expect, it } from 'vitest'

import { MAX_CONTENT, MAX_MEMORIES } from './memory'
import { excerpt, GuestRepo, RUN_STALE_MS, TEMPORARY_LIFETIME_MS } from './repo'
import { memoryStorage } from './storage'
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
    // The two it found, as the run was asked with them.
    await h.repo.storage.putMemory(memory('m-upd', 'Lives in Lisbon.', at))
    await h.repo.storage.putMemory(memory('m-del', 'Has a cat.', at))
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

  describe('at the end of a run', () => {
    /** A run that began with notes m1 and m2, in which the model rewrote m1, deleted m2 and added m3. */
    async function setup() {
      const h = harness()
      const c = await h.conversation()
      const turn = await h.turn(c, { ask: 'q', answer: 'a' })
      const began = h.tick()
      await h.repo.storage.putMemory(memory('m1', 'Lives in Lisbon.', began))
      await h.repo.storage.putMemory(memory('m2', 'Has a cat.', began))
      const at = h.tick()
      const end = {
        run: turn.run,
        memorySnapshot: null,
        // The server's list: the one the run began with, with the model's changes.
        memories: [memory('m1', 'Lives in Porto.', at), memory('m3', 'Likes jazz.', at)],
        memoryEvents: [
          memoryEvent('e1', 'm1', 'update', { before: 'Lives in Lisbon.', after: 'Lives in Porto.' }, at),
          memoryEvent('e2', 'm2', 'delete', { before: 'Has a cat.' }, at),
          memoryEvent('e3', 'm3', 'add', { after: 'Likes jazz.' }, at),
        ],
      }
      return { h, c, end }
    }
    const contents = async (h: Awaited<ReturnType<typeof setup>>['h']) => (await h.repo.memories()).map((m) => m.content).sort()

    it('applies the model\'s changes when the list was not touched meanwhile', async () => {
      const { h, c, end } = await setup()
      await h.repo.runEnded(c, end)
      expect(await contents(h)).toEqual(['Likes jazz.', 'Lives in Porto.'])
      expect((await h.repo.memoryEvents()).map((e) => e.id).sort()).toEqual(['e1', 'e2', 'e3'])
    })

    it('keeps a note the person wrote while the answer streamed', async () => {
      const { h, c, end } = await setup()
      await h.repo.createMemory('Drinks tea.')
      await h.repo.runEnded(c, end)
      expect(await contents(h)).toEqual(['Drinks tea.', 'Likes jazz.', 'Lives in Porto.'])
    })

    it('does not bring back a note the person deleted meanwhile, nor apply the model\'s change to it', async () => {
      const { h, c, end } = await setup()
      await h.repo.deleteMemory('m1')
      await h.repo.runEnded(c, end)
      expect(await contents(h)).toEqual(['Likes jazz.'])
      // The change that was not made is not in the undo list.
      expect((await h.repo.memoryEvents()).map((e) => e.id).sort()).toEqual(['e2', 'e3'])
    })

    it('leaves a note the person rewrote meanwhile as they wrote it, and keeps one the model deleted if they changed it', async () => {
      const { h, c, end } = await setup()
      await h.repo.updateMemory('m1', 'Lives in Faro.')
      await h.repo.updateMemory('m2', 'Has two cats.')
      await h.repo.runEnded(c, end)
      expect(await contents(h)).toEqual(['Has two cats.', 'Likes jazz.', 'Lives in Faro.'])
      expect((await h.repo.memoryEvents()).map((e) => e.id)).toEqual(['e3'])
    })

    it('is the same when the frame is delivered twice', async () => {
      const { h, c, end } = await setup()
      await h.repo.runEnded(c, end)
      await h.repo.runEnded(c, end)
      expect(await contents(h)).toEqual(['Likes jazz.', 'Lives in Porto.'])
      expect(await h.repo.memoryEvents()).toHaveLength(3)
    })

    it('does not run over a person\'s edit that is still being written', async () => {
      const { h, c, end } = await setup()
      // Both are queued at once: the edit first, then the end of the run.
      const [, ended] = await Promise.all([h.repo.updateMemory('m1', 'Lives in Faro.'), h.repo.runEnded(c, end)])
      expect(ended).toBeUndefined()
      expect(await contents(h)).toEqual(['Likes jazz.', 'Lives in Faro.'])
    })
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

describe('the addresses made for files', () => {
  /** A repository that counts the addresses it makes and lets go of, with one conversation holding a file. */
  async function withFile() {
    const made: string[] = []
    const revoked: string[] = []
    const repo = new GuestRepo(memoryStorage(), {
      urlFor: (_blob, id) => {
        made.push(id)
        return `blob:${id}`
      },
      revokeUrl: (url) => void revoked.push(url),
    })
    const c = (await repo.createConversation({})).id
    const at = new Date().toISOString()
    const input: Message = { id: 'q1', conversationId: c, role: 'user', content: 'see', parts: [{ type: 'text', text: 'see' }], status: 'complete', createdAt: at }
    const run: Run = { id: 'r1', conversationId: c, status: 'running', model: 'm', version: 1, createdAt: at }
    const file = { id: 'f1', conversationId: '', blob: new Blob(['x']), filename: 'a.png', contentType: 'image/png', size: 1 }
    await repo.runStarted(c, { run, input, action: 'send', attachments: [file], choice: {} }, 'tab')
    return { repo, c, made, revoked }
  }

  it('makes one address per file and lets go of it when the conversation is deleted', async () => {
    const { repo, c, made, revoked } = await withFile()
    await repo.listMessages(c, {})
    await repo.listMessages(c, {})
    expect(made).toEqual(['f1'])
    expect(revoked).toEqual([])

    await repo.deleteConversation(c)
    expect(revoked).toEqual(['blob:f1'])
  })

  it('lets go of them all when everything is deleted', async () => {
    const { repo, c, revoked } = await withFile()
    await repo.listMessages(c, {})
    await repo.deleteAll()
    expect(revoked).toEqual(['blob:f1'])
  })

  it('lets go of the address of a file that is deleted on its own, as when its question is edited away', async () => {
    const { repo, c, revoked } = await withFile()
    await repo.listMessages(c, {})
    const at = new Date(Date.now() + 1000).toISOString()
    const input: Message = { id: 'q2', conversationId: c, role: 'user', content: 'again', parts: [{ type: 'text', text: 'again' }], status: 'complete', createdAt: at }
    const run: Run = { id: 'r2', conversationId: c, status: 'running', model: 'm', version: 1, createdAt: at }
    await repo.runStarted(c, { run, input, action: 'send', replaces: 'q1', choice: {} }, 'tab')
    expect(revoked).toEqual(['blob:f1'])
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

// A message moderation refused is kept as a row of status blocked (keepBlocked):
// listed with the transcript, and in none of the places that read it as what was
// said - search, the export - as the server's queries leave it out.
describe('a refused message', () => {
  it('is kept after the newest row, whatever the clock says, and shown with the transcript', async () => {
    const h = harness()
    const c = await h.conversation()
    await h.turn(c, { ask: 'q1', answer: 'a1' })
    // A clock behind the server's rows, as a page's can be.
    h.clock.now -= 3_600_000
    const kept = await h.repo.keepBlocked(c, 'BAD')
    const page = await h.repo.listMessages(c, {})
    expect(page.items.map((m) => `${m.role}:${m.content}:${m.status}`)).toEqual(['user:q1:complete', 'assistant:a1:complete', 'user:BAD:blocked'])
    expect(page.items.at(-1)!.id).toBe(kept!.id)
    // It carries no run and no file.
    expect(page.items.at(-1)).not.toHaveProperty('runId')
    expect(page.items.at(-1)).not.toHaveProperty('attachments')
    // The conversation stops at it: a second does not go after the first.
    expect(await h.repo.keepBlocked(c, 'BAD AGAIN')).toBeNull()
    expect((await h.repo.listMessages(c, {})).items.map((m) => m.content)).toEqual(['q1', 'a1', 'BAD'])
  })

  it('is replaced by the words that were refused in its place, while it is still the last row', async () => {
    const h = harness()
    const c = await h.conversation()
    await h.turn(c, { ask: 'q1', answer: 'a1' })
    const kept = await h.repo.keepBlocked(c, 'BAD')
    const again = await h.repo.keepBlocked(c, 'BAD 2', h.tick(), kept!.id)
    expect(again).toMatchObject({ content: 'BAD 2', status: 'blocked' })
    expect(again!.id).not.toBe(kept!.id)
    expect((await h.repo.listMessages(c, {})).items.map((m) => m.content)).toEqual(['q1', 'a1', 'BAD 2'])

    // Not the last row any more (or not a refused message at all): nothing changes.
    await h.turn(c, { ask: 'q2', answer: 'a2', replaces: again!.id })
    expect(await h.repo.keepBlocked(c, 'BAD 3', undefined, again!.id)).toBeNull()
    const q1 = (await h.repo.data(c)).messages.find((m) => m.content === 'q1')!
    expect(await h.repo.keepBlocked(c, 'BAD 4', undefined, q1.id)).toBeNull()
    expect((await h.repo.listMessages(c, {})).items.map((m) => m.content)).toEqual(['q1', 'a1', 'q2', 'a2'])
  })

  it('is not found by a search and not in the export', async () => {
    const h = harness()
    const c = await h.conversation()
    await h.turn(c, { ask: 'ordinary words', answer: 'fine' })
    await h.repo.keepBlocked(c, 'forbidden words')

    expect((await h.repo.listConversations({ search: 'forbidden' })).items).toEqual([])
    expect((await h.repo.listConversations({ search: 'ordinary' })).items.map((i) => i.id)).toEqual([c])
    const exported = await h.repo.export()
    expect(exported.conversations[0]!.messages.map((m) => m.content)).toEqual(['ordinary words', 'fine'])
  })

  it('is deleted with the answer when the last question is answered again, and by an edit that reaches it', async () => {
    const h = harness()
    const c = await h.conversation()
    const q1 = await h.turn(c, { ask: 'q1', answer: 'a1' })
    await h.repo.keepBlocked(c, 'BAD')
    await h.turn(c, { action: 'regenerate', input: q1.input.id, answer: 'a1 again' })
    expect((await h.repo.data(c)).messages.map((m) => m.content)).toEqual(['q1', 'a1 again'])

    const again = await h.repo.keepBlocked(c, 'BAD 2')
    await h.turn(c, { ask: 'ok', answer: 'fine', replaces: again!.id })
    expect((await h.repo.data(c)).messages.map((m) => m.content)).toEqual(['q1', 'a1 again', 'ok', 'fine'])
  })

  it('does not keep a summary that ends at it', async () => {
    const h = harness()
    const c = await h.conversation()
    await h.turn(c, { ask: 'q1', answer: 'a1' })
    const kept = await h.repo.keepBlocked(c, 'BAD')
    const written = {
      content: 's', throughMessageId: kept!.id, model: 'm', inputTokens: 1, outputTokens: 1, createdAt: h.tick(),
    }
    expect(await h.repo.setSummary(c, written)).toBeUndefined()
  })
})

// The rows are the server's, in its clock. A row the page writes itself must be
// put among them by theirs, not by the page's - which can be minutes off.
describe.each([
  ['five minutes ahead', 5 * 60_000],
  ['five minutes behind', -5 * 60_000],
])('the page\'s clock %s of the server\'s', (_name, skew) => {
  const order = async (h: ReturnType<typeof harness>, c: string) => (await h.repo.listMessages(c, {})).items.map((m) => m.content)

  it.each([
    ['with the time the server gave', true],
    ['without it', false],
  ])('keeps a refused message where it was said, %s', async (_how, told) => {
    const h = harness(skew)
    const c = await h.conversation()
    await h.turn(c, { ask: 'A', answer: 'a' })
    const at = h.tick() // the server's time of the refusal, after A and its answer
    await h.repo.keepBlocked(c, 'BAD', told ? at : undefined)
    await h.turn(c, { ask: 'B', answer: 'b' })
    expect(await order(h, c)).toEqual(['A', 'a', 'BAD', 'B', 'b'])
  })

  it('is edited away with what came after it, and what came after is not sent as context', async () => {
    const h = harness(skew)
    const c = await h.conversation()
    await h.turn(c, { ask: 'A', answer: 'a' })
    const refused = await h.repo.keepBlocked(c, 'BAD', h.tick())
    await h.turn(c, { ask: 'B', answer: 'b' })
    // The edit replaces the refused message and every row after it.
    await h.turn(c, { ask: 'ok', answer: 'fine', replaces: refused!.id })
    expect(await order(h, c)).toEqual(['A', 'a', 'ok', 'fine'])
  })

  it('puts the failed reply of an interrupted run before the question asked next', async () => {
    const h = harness(skew)
    const c = await h.conversation()
    const first = await h.turn(c, { ask: 'A', status: 'running' })
    const lost = await h.repo.runInterrupted(c, first.run.id, 'connection_lost')
    expect(lost).not.toBeNull()
    await h.turn(c, { ask: 'B', answer: 'b' })
    expect((await h.repo.listMessages(c, {})).items.map((m) => `${m.role}:${m.content}`)).toEqual(['user:A', 'assistant:', 'user:B', 'assistant:b'])
    // Its run ended when the reply did, not at the page's time.
    expect(Date.parse(lost!.run.finishedAt!)).toBe(Date.parse(lost!.message.createdAt))
  })
})

describe('a refused message in a conversation with nothing else', () => {
  it('goes before whatever the server writes next, however far ahead the page\'s clock is', async () => {
    const h = harness(5 * 60_000)
    const c = await h.conversation()
    await h.repo.keepBlocked(c, 'BAD')
    await h.turn(c, { ask: 'B', answer: 'b' })
    expect((await h.repo.listMessages(c, {})).items.map((m) => m.content)).toEqual(['BAD', 'B', 'b'])
  })

  it('keeps the server\'s time unless it is not after the newest row', async () => {
    const h = harness(5 * 60_000)
    const c = await h.conversation()
    const turn = await h.turn(c, { ask: 'A', answer: 'a' })
    const newest = turn.rows.at(-1)!.createdAt
    // A time the rows are already past (another server's clock): just after the newest row.
    const early = new Date(Date.parse(newest) - 60_000).toISOString()
    const kept = await h.repo.keepBlocked(c, 'BAD', early)
    expect(Date.parse(kept!.createdAt)).toBe(Date.parse(newest) + 1)
    const told = h.tick()
    const later = await h.repo.keepBlocked(c, 'BAD 2', told, kept!.id)
    expect(later!.createdAt).toBe(told)
  })
})
