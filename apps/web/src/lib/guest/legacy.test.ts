import { describe, expect, it } from 'vitest'

import { isBranched, pruneBranches } from './legacy'
import { harness } from './testing'
import type { StoredConversation, StoredMessage, StoredRun } from './types'

// Data kept by a version with branches (a tree of questions, answers set aside
// by a regeneration) is brought to a line the first time it is read: exactly
// what the reader saw stays, the rest is deleted, and the fields of the tree
// are gone. The server's 20261020000001 migration, over the browser's storage.

let n = 0
const at = () => new Date(Date.parse('2026-10-01T00:00:00Z') + ++n * 1000).toISOString()

const user = (id: string, parent: string | null, extra: Record<string, unknown> = {}) =>
  ({
    id, conversationId: 'c', role: 'user', content: id, parts: [{ type: 'text', text: id }], status: 'complete', createdAt: at(),
    parentInputId: parent, shownAt: 1, ...extra,
  }) as StoredMessage
const answer = (id: string, runId: string) =>
  ({ id, conversationId: 'c', role: 'assistant', content: id, parts: [{ type: 'text', text: id }], status: 'complete', createdAt: at(), runId, stopReason: 'stop' }) as StoredMessage
const run = (id: string, inputId: string, extra: Record<string, unknown> = {}) =>
  ({ id, conversationId: 'c', status: 'completed', model: 'm', version: 2, createdAt: at(), inputId, superseded: false, ...extra }) as StoredRun
const conversation = (currentInputId: string | null) =>
  ({
    id: 'c', title: 't', model: 'm', reasoningEffort: 'auto', webSearch: true, temporary: false,
    createdAt: at(), updatedAt: at(), currentInputId, memorySnapshot: null,
  }) as unknown as StoredConversation

/** The branched conversation of the tests: q2 was edited into q2b (after a q3 had been asked), and q3b regenerated. */
function branched() {
  const messages = [
    user('q1', null),
    answer('a1', 'r1'),
    user('q2', 'q1', { attachments: [{ id: 'f-q2', filename: 'a.png', contentType: 'image/png', size: 1 }] }),
    answer('a2', 'r2'),
    user('q3', 'q2', { attachments: [{ id: 'f-q3', filename: 'b.png', contentType: 'image/png', size: 1 }] }),
    answer('a3', 'r3'),
    user('q2b', 'q1', { editOf: 'q2', attachments: [{ id: 'f-q2', filename: 'a.png', contentType: 'image/png', size: 1 }] }),
    answer('a2b', 'r2b'),
    user('q3b', 'q2b'),
    answer('a3b-old', 'r3b-old'),
    answer('a3b', 'r3b'),
  ]
  const runs = [
    run('r1', 'q1'), run('r2', 'q2'), run('r3', 'q3'), run('r2b', 'q2b'),
    run('r3b-old', 'q3b', { superseded: true }), run('r3b', 'q3b'),
  ]
  return { conversation: conversation('q3b'), messages, runs }
}

describe('pruneBranches', () => {
  it('keeps what the reader saw, without the fields of the tree', () => {
    const { conversation, messages, runs } = branched()
    const pruned = pruneBranches(conversation, messages, runs)
    expect(pruned.messages.map((m) => m.id)).toEqual(['q1', 'a1', 'q2b', 'a2b', 'q3b', 'a3b'])
    expect(pruned.runs.map((r) => r.id).sort()).toEqual(['r1', 'r2b', 'r3b'])
    expect(pruned.deletedMessages.sort()).toEqual(['a2', 'a3', 'a3b-old', 'q2', 'q3'])
    expect(pruned.deletedRuns.sort()).toEqual(['r2', 'r3', 'r3b-old'])
    for (const m of pruned.messages) {
      expect(m).not.toHaveProperty('parentInputId')
      expect(m).not.toHaveProperty('shownAt')
      expect(m).not.toHaveProperty('editOf')
    }
    for (const r of pruned.runs) expect(r).not.toHaveProperty('superseded')
    expect('currentInputId' in pruned.conversation).toBe(false)
  })

  it('removes the files of what went, and keeps one a kept question still carries', () => {
    const { conversation, messages, runs } = branched()
    // f-q2 is on q2 (deleted) and on q2b (kept); f-q3 only on q3.
    expect(pruneBranches(conversation, messages, runs).deletedFiles).toEqual(['f-q3'])
  })

  it('keeps a reply that asked and went on, and deletes one that was regenerated', () => {
    const messages = [user('q', null), answer('ask-old', 'h1'), answer('went-old', 'c1'), answer('ask', 'h2'), answer('went', 'c2')]
    const runs = [
      run('h1', 'q', { superseded: true }), run('c1', 'q', { superseded: true, continues: 'h1' }),
      run('h2', 'q'), run('c2', 'q', { continues: 'h2' }),
    ]
    const pruned = pruneBranches(conversation('q'), messages, runs)
    expect(pruned.messages.map((m) => m.id)).toEqual(['q', 'ask', 'went'])
    expect(pruned.runs.map((r) => r.id).sort()).toEqual(['c2', 'h2'])
  })

  it('keeps a message moderation refused, which names no parent and is on no path', () => {
    const messages = [user('q1', null), answer('a1', 'r1'), user('b1', null, { status: 'blocked' })]
    const pruned = pruneBranches(conversation('q1'), messages, [run('r1', 'q1')])
    expect(pruned.messages.map((m) => m.id)).toEqual(['q1', 'a1', 'b1'])
    expect(pruned.deletedMessages).toEqual([])
  })

  it('leaves a linear conversation as it was, row for row', () => {
    const messages = [user('q1', null), answer('a1', 'r1'), user('q2', 'q1'), answer('a2', 'r2')]
    const runs = [run('r1', 'q1'), run('r2', 'q2')]
    const pruned = pruneBranches(conversation('q2'), messages, runs)
    expect(pruned.messages.map((m) => m.id)).toEqual(['q1', 'a1', 'q2', 'a2'])
    expect(pruned.deletedMessages).toEqual([])
    expect(pruned.deletedRuns).toEqual([])
    expect(pruned.deletedFiles).toEqual([])
  })

  it('recognises a conversation kept with a current leaf, a null one included', () => {
    expect(isBranched(conversation(null))).toBe(true)
    expect(isBranched(conversation('q'))).toBe(true)
    const { currentInputId: _gone, ...fresh } = conversation('q') as StoredConversation & { currentInputId: string }
    void _gone
    expect(isBranched(fresh as StoredConversation)).toBe(false)
  })
})

describe('reading old data', () => {
  it('prunes a branched conversation when the repository first reads it, once', async () => {
    const h = harness()
    const { conversation, messages, runs } = branched()
    await h.repo.storage.putConversation(conversation)
    await h.repo.storage.putMessages(messages)
    await h.repo.storage.putRuns(runs)
    // A linear conversation next to it, kept the old way too.
    const other = { ...conversation, id: 'c2', currentInputId: null }
    await h.repo.storage.putConversation(other)

    const page = await h.repo.listMessages('c', {})
    expect(page.items.map((m) => m.id)).toEqual(['q1', 'a1', 'q2b', 'a2b', 'q3b', 'a3b'])
    const data = await h.repo.data('c')
    expect(data.messages).toHaveLength(6)
    expect(data.runs).toHaveLength(3)
    expect('currentInputId' in data.conversation).toBe(false)
    expect('currentInputId' in (await h.repo.data('c2')).conversation).toBe(false)
    // Stored that way, so that it is not looked at again.
    expect(await h.repo.storage.messages('c')).toHaveLength(6)
  })

  it('does not find in a search what the old data held off the path', async () => {
    const h = harness()
    const { conversation, messages, runs } = branched()
    await h.repo.storage.putConversation(conversation)
    await h.repo.storage.putMessages(messages)
    await h.repo.storage.putRuns(runs)
    expect((await h.repo.listConversations({ search: 'q3' })).items.map((c) => c.id)).toEqual(['c'])
    expect((await h.repo.listConversations({ search: 'a3b-old' })).items).toEqual([])
    expect((await h.repo.listConversations({ search: 'a2' })).items.map((c) => c.id)).toEqual(['c'])
    expect((await h.repo.listConversations({ search: 'a2b' })).items[0]!.match?.messageId).toBe('a2b')
  })
})
