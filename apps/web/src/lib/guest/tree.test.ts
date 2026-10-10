import { describe, expect, it } from 'vitest'

import { GuestError } from './errors'
import { harness, type Harness } from './testing'
import { chooseAnswer, pageOf, pickBranchLeaf, timeKey } from './tree'

// The tree rules (AGENTS.md, "对话是一棵树"), as the guest's browser applies
// them: the transcript is the path to the current leaf with each input's one
// shown answer; an edit is a sibling and moves the leaf; switching lands on the
// branch last shown; regenerating sets answers aside; the last answer can be
// chosen again.

const texts = async (h: Harness, id: string) => (await h.repo.listMessages(id, {})).items.map((m) => `${m.role}:${m.content}`)

describe('the transcript', () => {
  it('is the path to the current leaf with each input\'s answer', async () => {
    const h = harness()
    const c = await h.conversation()
    await h.turn(c, { ask: 'q1', answer: 'a1' })
    await h.turn(c, { ask: 'q2', answer: 'a2', tools: true })
    const page = await h.repo.listMessages(c, {})
    expect(page.items.map((m) => m.role)).toEqual(['user', 'assistant', 'user', 'assistant', 'tool', 'assistant'])
    expect(page.olderCursor).toBeNull()
    // What a provider wants back is the runner's to hold, never the page's.
    expect(page.items.every((m) => !('providerState' in m))).toBe(true)
  })

  it('sets an edit beside the question it rewords and moves the leaf onto it', async () => {
    const h = harness()
    const c = await h.conversation()
    const first = await h.turn(c, { ask: 'q1', answer: 'a1' })
    const second = await h.turn(c, { ask: 'q2', answer: 'a2' })
    // q2 reworded: a sibling under q1, `editOf` the first version.
    const edited = await h.turn(c, { ask: 'q2 again', answer: 'a2 again', parent: first.input.id, editOf: second.input.id })

    expect(await texts(h, c)).toEqual(['user:q1', 'assistant:a1', 'user:q2 again', 'assistant:a2 again'])
    const page = await h.repo.listMessages(c, {})
    const shown = page.items.find((m) => m.id === edited.input.id)!
    expect(shown.branches).toEqual([second.input.id, edited.input.id])
    // The first question has no other version.
    expect(page.items.find((m) => m.id === first.input.id)!.branches).toBeUndefined()
  })

  it('switches to the other version and lands where that branch was last shown', async () => {
    const h = harness()
    const c = await h.conversation()
    const q1 = await h.turn(c, { ask: 'q1', answer: 'a1' })
    const q2 = await h.turn(c, { ask: 'q2', answer: 'a2' })
    const q3 = await h.turn(c, { ask: 'q3', answer: 'a3' })
    // q1 reworded; the branch under the original (q2, q3) is off the path.
    await h.turn(c, { ask: 'q1 again', answer: 'b1', parent: null, editOf: q1.input.id })
    expect(await texts(h, c)).toEqual(['user:q1 again', 'assistant:b1'])

    // Back to the original: its branch comes back whole, ending at q3.
    const page = await h.repo.switchVersion(c, q1.input.id)
    expect(page.items.map((m) => m.content)).toEqual(['q1', 'a1', 'q2', 'a2', 'q3', 'a3'])
    expect(page.items.find((m) => m.id === q1.input.id)!.branches).toHaveLength(2)

    // And forward again, to where the reworded branch was left.
    const forward = await h.repo.switchVersion(c, (await h.repo.data(c)).messages.find((m) => m.content === 'q1 again')!.id)
    expect(forward.items.map((m) => m.content)).toEqual(['q1 again', 'b1'])
    expect(q2.input.id).not.toBe(q3.input.id)
  })

  it('lands on the leaf shown last when a version has several', async () => {
    const h = harness()
    const c = await h.conversation()
    const q1 = await h.turn(c, { ask: 'q1', answer: 'a1' })
    const left = await h.turn(c, { ask: 'left', answer: 'al' })
    // Two children of q1: `left` and `right`; right was the branch shown last.
    const right = await h.turn(c, { ask: 'right', answer: 'ar', parent: q1.input.id, editOf: left.input.id })
    await h.turn(c, { ask: 'q1 reworded', answer: 'b1', parent: null, editOf: q1.input.id })

    const data = await h.repo.data(c)
    expect(pickBranchLeaf(data.messages, q1.input.id)).toBe(right.input.id)
    const page = await h.repo.switchVersion(c, q1.input.id)
    expect(page.items.map((m) => m.content)).toEqual(['q1', 'a1', 'right', 'ar'])
  })

  it('refuses to switch while a run is going, or to a question off the path', async () => {
    const h = harness()
    const c = await h.conversation()
    const q1 = await h.turn(c, { ask: 'q1', answer: 'a1' })
    const q2 = await h.turn(c, { ask: 'q2', answer: 'a2' })
    // q1's branch was left; its q2 is no sibling of anything on the path now.
    await h.turn(c, { ask: 'q1 again', answer: 'b1', parent: null, editOf: q1.input.id })
    await expect(h.repo.switchVersion(c, q2.input.id)).rejects.toMatchObject({ code: 'not_on_path', status: 409 })
    await expect(h.repo.switchVersion(c, 'nope')).rejects.toMatchObject({ code: 'message_not_found', status: 404 })

    await h.turn(c, { ask: 'q2 live', parent: (await h.repo.data(c)).conversation.currentInputId, status: 'running' })
    await expect(h.repo.switchVersion(c, q1.input.id)).rejects.toMatchObject({ code: 'conversation_busy', status: 409 })
  })
})

describe('answers', () => {
  it('sets the earlier answers aside when regenerating and keeps them as versions', async () => {
    const h = harness()
    const c = await h.conversation()
    const q = await h.turn(c, { ask: 'q', answer: 'first' })
    const again = await h.turn(c, { action: 'regenerate', input: q.input.id, answer: 'second' })

    expect(await texts(h, c)).toEqual(['user:q', 'assistant:second'])
    const input = (await h.repo.listMessages(c, {})).items[0]!
    expect(input.versions).toEqual([q.run.id, again.run.id])

    // Choosing the first brings it back and hides the second.
    const chosen = await h.repo.chooseAnswer(c, q.run.id)
    expect(chosen.items.map((m) => m.content)).toEqual(['q', 'first'])
    expect(await texts(h, c)).toEqual(['user:q', 'assistant:first'])
  })

  it('treats a run that goes on after a question as part of one version', async () => {
    const h = harness()
    const c = await h.conversation()
    const q = await h.turn(c, { ask: 'q', answer: 'which one?', status: 'awaiting_input' })
    const resumed = await h.turn(c, { action: 'answer', input: q.input.id, continues: q.run.id, answer: 'done' })

    expect(await texts(h, c)).toEqual(['user:q', 'assistant:which one?', 'assistant:done'])
    const input = (await h.repo.listMessages(c, {})).items[0]!
    expect(input.versions).toBeUndefined() // one version, named by the run that began it
    // Regenerating hides both halves together.
    await h.turn(c, { action: 'regenerate', input: q.input.id, answer: 'fresh' })
    expect(await texts(h, c)).toEqual(['user:q', 'assistant:fresh'])
    expect((await h.repo.listMessages(c, {})).items[0]!.versions).toEqual([q.run.id, (await h.repo.data(c)).runs.find((r) => !r.continues && r.id !== q.run.id)!.id])
    expect(resumed.run.continues).toBe(q.run.id)
  })

  it('lets only the last question\'s answer be chosen', async () => {
    const h = harness()
    const c = await h.conversation()
    const q1 = await h.turn(c, { ask: 'q1', answer: 'a1' })
    await h.turn(c, { action: 'regenerate', input: q1.input.id, answer: 'a1b' })
    await h.turn(c, { ask: 'q2', answer: 'a2' })
    await expect(h.repo.chooseAnswer(c, q1.run.id)).rejects.toMatchObject({ code: 'not_latest', status: 409 })
    await expect(h.repo.chooseAnswer(c, 'nope')).rejects.toMatchObject({ code: 'run_not_found', status: 404 })
  })

  it('chooseAnswer is pure over runs', () => {
    const runs = [
      { id: 'a', inputId: 'i', superseded: false },
      { id: 'b', inputId: 'i', superseded: true, continues: 'a' },
      { id: 'c', inputId: 'j', superseded: false },
    ] as never
    expect((chooseAnswer(runs, 'i', null) as { superseded: boolean }[]).map((r) => r.superseded)).toEqual([true, true, false])
    expect((chooseAnswer(runs, 'i', 'a') as { superseded: boolean }[]).map((r) => r.superseded)).toEqual([false, false, false])
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
