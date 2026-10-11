import { describe, expect, it } from 'vitest'

import { assistant, id, summary, toolCallRow, toolResultRow, user } from '../testing'
import { planCompaction, rowsToSend, sentToModel, survivingSummary } from './summary'

// A guest's summary follows the rules of the server's (internal/agent): it
// stands in for the rows up to its last one, keeps a question as written, and
// goes with the rows it is tied to.

const rows = (...names: string[]) =>
  names.map((n) => (n.startsWith('q') ? user(n) : assistant(n, id('run'))))

describe('survivingSummary', () => {
  it('stays while the row it ends at and the question it kept are there', () => {
    const s = summary('a2', { keptMessageId: id('q2') })
    expect(survivingSummary(s, rows('q1', 'a1', 'q2', 'a2'))).toBe(s)
  })

  it('goes with the row it ends at: an edit inside the range, a regenerated answer it covered', () => {
    const s = summary('a2')
    // Editing q2 deleted q2 and everything after it.
    expect(survivingSummary(s, rows('q1', 'a1'))).toBeUndefined()
    // Regenerating the last question deleted its answer.
    expect(survivingSummary(s, rows('q1', 'a1', 'q2'))).toBeUndefined()
  })

  it('goes with the question it kept', () => {
    const s = summary('a2', { keptMessageId: id('q2') })
    expect(survivingSummary(s, rows('q1', 'a1', 'a2'))).toBeUndefined()
  })

  it('survives an edit after its range, and a regeneration that leaves the row it ends at', () => {
    const s = summary('a1')
    expect(survivingSummary(s, rows('q1', 'a1', 'q2'))).toBe(s)
    // Ending at the question that is regenerated: the answer deleted was not in it.
    const atQuestion = summary('q2', { keptMessageId: id('q2') })
    expect(survivingSummary(atQuestion, rows('q1', 'a1', 'q2'))).toBe(atQuestion)
  })

  it('is nothing without a summary', () => {
    expect(survivingSummary(undefined, rows('q1'))).toBeUndefined()
    expect(survivingSummary(null, rows('q1'))).toBeUndefined()
  })
})

describe('rowsToSend', () => {
  const all = rows('q1', 'a1', 'q2', 'a2', 'q3', 'a3', 'q4')

  it('is the newest rows without a summary', () => {
    expect(rowsToSend(all, undefined, 3).map((m) => m.id)).toEqual([id('a3'), id('q4')].slice(-2).length ? [id('q3'), id('a3'), id('q4')] : [])
  })

  it('sends the kept question, the row the summary ends at, and what follows, not the rows between', () => {
    const s = summary('a3', { keptMessageId: id('q2') })
    expect(rowsToSend(all, s, 300).map((m) => m.id)).toEqual([id('q2'), id('a3'), id('q4')])
  })

  it('sends the row once when it is also the one kept', () => {
    const s = summary('q4', { keptMessageId: id('q4') })
    expect(rowsToSend(all, s, 300).map((m) => m.id)).toEqual([id('q4')])
  })

  it('sends only the row it ends at when it kept none', () => {
    expect(rowsToSend(all, summary('a2'), 300).map((m) => m.id)).toEqual([id('a2'), id('q3'), id('a3'), id('q4')])
  })

  it('cuts what follows, never the two rows the summary needs', () => {
    const s = summary('a1', { keptMessageId: id('q1') })
    expect(rowsToSend(all, s, 4).map((m) => m.id)).toEqual([id('q1'), id('a1'), id('a3'), id('q4')])
  })

  it('falls back to the newest rows for a summary of rows that are not there', () => {
    expect(rowsToSend(all, summary('gone'), 2).map((m) => m.id)).toEqual([id('a3'), id('q4')])
  })
})

describe('planCompaction', () => {
  it('covers everything, the last question included, when it has its answer', () => {
    const plan = planCompaction(rows('q1', 'a1', 'q2', 'a2'), undefined)!
    expect(plan.through.id).toBe(id('a2'))
    expect(plan.keep).toBeUndefined()
    expect(plan.covered.map((m) => m.id)).toEqual([id('q1'), id('a1'), id('q2'), id('a2')])
  })

  it('keeps a question that has no answer, and ends the summary at it', () => {
    const failed = assistant('f', id('run'), { stopReason: 'error', parts: [], errorCode: 'context_too_long' })
    const plan = planCompaction([...rows('q1', 'a1', 'q2'), failed], undefined)!
    expect(plan.through.id).toBe(id('q2'))
    expect(plan.keep?.id).toBe(id('q2'))
    expect(plan.covered.map((m) => m.id)).toEqual([id('q1'), id('a1')])
  })

  it('counts a stopped answer as no answer too, and a tool round that went through as one', () => {
    const stopped = assistant('s', id('run'), { stopReason: 'cancelled' })
    expect(planCompaction([...rows('q1'), stopped], undefined)!.keep?.id).toBe(id('q1'))

    const tools = [user('q'), toolCallRow('c', id('run'), 'call-1'), toolResultRow('t', id('run'), 'call-1')]
    const plan = planCompaction(tools, undefined)!
    expect(plan.keep).toBeUndefined()
    expect(plan.through.id).toBe(id('t'))
  })

  it('reads on from the old summary, and folds the question it kept into the history', () => {
    const s = summary('a2', { keptMessageId: id('q2') })
    const plan = planCompaction(rows('q1', 'a1', 'q2', 'a2', 'q3', 'a3'), s)!
    expect(plan.through.id).toBe(id('a3'))
    expect(plan.covered.map((m) => m.id)).toEqual([id('q2'), id('q3'), id('a3')])
  })

  it('is nothing when only failures came since a summary that kept the question', () => {
    // Compressing again, while the answer still fails: there is nothing to add,
    // and folding the question into the summary would take it from the answer.
    const s = summary('q2', { keptMessageId: id('q2') })
    const failed = assistant('f', id('run'), { stopReason: 'error', parts: [] })
    expect(planCompaction([...rows('q1', 'a1', 'q2'), failed], s)).toBeNull()
  })

  it('is nothing when nothing came after the summary, or when there is nothing', () => {
    expect(planCompaction(rows('q1', 'a1'), summary('a1'))).toBeNull()
    expect(planCompaction([], undefined)).toBeNull()
  })

  it('ignores a summary of rows that are gone', () => {
    const plan = planCompaction(rows('q1', 'a1'), summary('gone'))!
    expect(plan.covered).toHaveLength(2)
  })
})

describe('sentToModel', () => {
  it('is an answer with something in it that did not fail or stop', () => {
    expect(sentToModel(assistant('a', id('run')))).toBe(true)
    expect(sentToModel(assistant('a', id('run'), { stopReason: 'error' }))).toBe(false)
    expect(sentToModel(assistant('a', id('run'), { stopReason: 'cancelled' }))).toBe(false)
    expect(sentToModel(assistant('a', id('run'), { parts: [], content: '' }))).toBe(false)
    expect(sentToModel(user('q'))).toBe(false)
  })
})

// A message moderation refused (status blocked) is a row of the transcript and
// no row of any of this: the server's queries leave it out.
describe('a refused message', () => {
  const blocked = (name: string) => user(name, { status: 'blocked' })

  it('is never sent, whether or not there is a summary', () => {
    const all = [...rows('q1', 'a1'), blocked('b1'), ...rows('q2', 'a2'), blocked('b2')]
    expect(rowsToSend(all, undefined, 300).map((m) => m.id)).toEqual([id('q1'), id('a1'), id('q2'), id('a2')])
    // The window is counted in rows that can be sent.
    expect(rowsToSend(all, undefined, 2).map((m) => m.id)).toEqual([id('q2'), id('a2')])
    const s = summary('a1', { keptMessageId: id('q1') })
    expect(rowsToSend(all, s, 300).map((m) => m.id)).toEqual([id('q1'), id('a1'), id('q2'), id('a2')])
  })

  it('is never what a summary ends at or keeps', () => {
    const all = [...rows('q1', 'a1'), blocked('b1')]
    expect(survivingSummary(summary('b1'), all)).toBeUndefined()
    expect(survivingSummary(summary('a1', { keptMessageId: id('b1') }), all)).toBeUndefined()
    const fine = summary('a1')
    expect(survivingSummary(fine, all)).toBe(fine)
  })

  it('is not covered by a compression, and does not end it', () => {
    const plan = planCompaction([...rows('q1', 'a1'), blocked('b1'), ...rows('q2', 'a2'), blocked('b2')], undefined)!
    expect(plan.through.id).toBe(id('a2'))
    expect(plan.covered.map((m) => m.id)).toEqual([id('q1'), id('a1'), id('q2'), id('a2')])
    // A question without an answer is the last question even with a refusal after it.
    const unanswered = planCompaction([...rows('q1', 'a1', 'q2'), blocked('b1')], undefined)!
    expect(unanswered.through.id).toBe(id('q2'))
    expect(unanswered.keep?.id).toBe(id('q2'))
  })

  it('is nothing new to compress when it is all that came after the summary', () => {
    expect(planCompaction([...rows('q1', 'a1'), blocked('b1')], summary('a1'))).toBeNull()
    expect(planCompaction([blocked('b1')], undefined)).toBeNull()
  })
})
