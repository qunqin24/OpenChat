import { describe, expect, it } from 'vitest'

import { ASK_USER } from './ask-user'
import { assistant, id, liveRun, run, text, toolCallRow, toolResultRow, user } from './testing'
import { buildTurns, pendingQuestion, turnText, type AssistantTurn } from './transcript'

const assistantTurns = (turns: ReturnType<typeof buildTurns>) =>
  turns.filter((t): t is AssistantTurn => t.kind === 'assistant')

describe('buildTurns', () => {
  it('folds a run that used tools into one assistant block', () => {
    const rows = [
      user('q'),
      toolCallRow('c1', id('r1'), 'call-1'),
      toolResultRow('t1', id('r1'), 'call-1'),
      toolCallRow('c2', id('r1'), 'call-2'),
      toolResultRow('t2', id('r1'), 'call-2'),
      assistant('final', id('r1')),
    ]
    const turns = buildTurns(rows, null)
    expect(turns.map((t) => t.kind)).toEqual(['user', 'assistant'])
    const reply = assistantTurns(turns)[0]!
    expect(reply.key).toBe(id('r1'))
    expect(reply.responses).toHaveLength(3)
    expect(Object.keys(reply.results).sort()).toEqual(['call-1', 'call-2'])
    expect(reply.runs).toEqual([id('r1')])
    // The tool-call rows carry no text of their own.
    expect(turnText(reply)).toBe('final')
  })

  it('gives each run its own block when a user turn separates them', () => {
    const turns = buildTurns(
      [user('q1'), assistant('a1', id('r1')), user('q2'), assistant('a2', id('r2'))],
      null,
    )
    expect(turns.map((t) => t.kind)).toEqual(['user', 'assistant', 'user', 'assistant'])
    expect(assistantTurns(turns).map((t) => t.key)).toEqual([id('r1'), id('r2')])
  })

  it('skips roles that are not drawn', () => {
    const system = { ...user('s'), role: 'system' } as unknown as ReturnType<typeof user>
    expect(buildTurns([system], null)).toEqual([])
  })

  describe('with a run in flight', () => {
    it('adds a block for the running reply before its first response', () => {
      const r = run('r1')
      const q = user('q')
      const turns = buildTurns([], liveRun(r, [q]))
      expect(turns.map((t) => t.kind)).toEqual(['user', 'assistant'])
      const reply = assistantTurns(turns)[0]!
      expect(reply.key).toBe(r.id)
      expect(reply.live?.run.id).toBe(r.id)
      expect(reply.partial).toBeNull()
    })

    it('shows the streaming partial on the run block and does not repeat persisted rows', () => {
      const r = run('r1')
      const q = user('q')
      const answer = assistant('a', r.id)
      const turns = buildTurns([q, answer], liveRun(r, [q, answer], { partial: [text('more')] }))
      expect(turns.map((t) => t.kind)).toEqual(['user', 'assistant'])
      const reply = assistantTurns(turns)[0]!
      expect(reply.responses).toHaveLength(1)
      expect(reply.partial).toEqual([text('more')])
    })

    it('draws nothing for a run that is over and has no rows yet', () => {
      const r = run('r1', { status: 'failed' })
      const turns = buildTurns([], liveRun(r, [user('q')]))
      expect(turns.map((t) => t.kind)).toEqual(['user'])
    })

    it('hides the old answer while the same question is answered again', () => {
      const q = user('q')
      const old = assistant('old', id('r1'))
      const r2 = run('r2')
      const turns = buildTurns([q, old], liveRun(r2, [q]))
      const replies = assistantTurns(turns)
      expect(replies).toHaveLength(1)
      expect(replies[0]!.key).toBe(r2.id)
      expect(replies[0]!.responses).toHaveLength(0)
    })

    it('keeps the rows of the run itself when it redoes a question', () => {
      const q = user('q')
      const old = assistant('old', id('r1'))
      const r2 = run('r2')
      const fresh = assistant('fresh', r2.id)
      const turns = buildTurns([q, old, fresh], liveRun(r2, [q, fresh]))
      const replies = assistantTurns(turns)
      expect(replies.map((t) => t.responses.map((m) => m.id))).toEqual([[fresh.id]])
    })

    it('leaves the reply before a question alone when the run continues it', () => {
      const q = user('q')
      const asked = toolCallRow('ask', id('r1'), 'call-ask', ASK_USER, { questions: [] })
      const result = toolResultRow('ans', id('r1'), 'call-ask', ASK_USER)
      const r2 = run('r2', { continues: id('r1') })
      const after = assistant('after', r2.id)
      const turns = buildTurns([q, asked, result], liveRun(r2, [after]))
      const replies = assistantTurns(turns)
      expect(replies).toHaveLength(1)
      expect(replies[0]!.key).toBe(id('r1'))
      expect(replies[0]!.runs).toEqual([id('r1'), r2.id])
      expect(replies[0]!.responses.map((m) => m.id)).toEqual([asked.id, after.id])
    })
  })

  describe('replacing', () => {
    const q1 = user('q1')
    const a1 = assistant('a1', id('r1'))
    const q2 = user('q2')
    const a2 = assistant('a2', id('r2'))

    it('hides the rows after the question being answered again', () => {
      const turns = buildTurns([q1, a1, q2, a2], null, { after: q2.id })
      expect(turns.map((t) => (t.kind === 'user' ? t.message.id : t.key))).toEqual([q1.id, id('r1'), q2.id])
    })

    it('hides nothing when `after` is not in the list', () => {
      expect(buildTurns([q1, a1], null, { after: id('nope') })).toHaveLength(2)
    })

    it('shows the run that follows a redo and not the rows it deleted', () => {
      const edited = user('q2b')
      const r3 = run('r3')
      const turns = buildTurns([q1, a1, q2, a2], liveRun(r3, [edited]), { from: q2.id })
      expect(turns.map((t) => (t.kind === 'user' ? t.message.id : t.key))).toEqual([q1.id, id('r1'), edited.id, r3.id])
    })

    it('hides the question named by `from` and everything after it', () => {
      const turns = buildTurns([q1, a1, q2, a2], null, { from: q2.id })
      expect(turns.map((t) => (t.kind === 'user' ? t.message.id : t.key))).toEqual([q1.id, id('r1')])
    })

    it('hides nothing when `from` is not in the list', () => {
      expect(buildTurns([q1, a1], null, { from: id('nope') })).toHaveLength(2)
    })

    it('hides the whole transcript when `from` is the first question', () => {
      expect(buildTurns([q1, a1, q2, a2], null, { from: q1.id })).toEqual([])
    })
  })
})

describe('pendingQuestion', () => {
  const ask = (answered: boolean, stopReason: 'tool_calls' | 'stop' = 'tool_calls') => {
    const rows = [
      user('q'),
      { ...toolCallRow('ask', id('r1'), 'call-ask', ASK_USER), stopReason },
      ...(answered ? [toolResultRow('ans', id('r1'), 'call-ask', ASK_USER)] : []),
    ]
    return buildTurns(rows, null)
  }

  it('names the unanswered ask_user call', () => {
    expect(pendingQuestion(ask(false), false)).toBe('call-ask')
  })

  it('is null once the call has a result', () => {
    expect(pendingQuestion(ask(true), false)).toBeNull()
  })

  it('is null while something else is going on', () => {
    expect(pendingQuestion(ask(false), true)).toBeNull()
  })

  it('is null when the reply did not stop on tool calls', () => {
    expect(pendingQuestion(ask(false, 'stop'), false)).toBeNull()
  })

  it('ignores other tools left without a result', () => {
    const turns = buildTurns([user('q'), toolCallRow('c', id('r1'), 'call-1', 'web_search')], null)
    expect(pendingQuestion(turns, false)).toBeNull()
  })

  it('is null when the last turn is the user’s', () => {
    expect(pendingQuestion(buildTurns([user('q')], null), false)).toBeNull()
  })
})
