import { describe, expect, it } from 'vitest'

import { ASK_USER } from './ask-user'
import { assistant, id, liveRun, run, summary, text, toolCallRow, toolResultRow, user } from './testing'
import { buildTurns, isRefusedTurn, pendingQuestion, refusedTail, sentTurns, turnText, type AssistantTurn } from './transcript'

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

describe('buildTurns with a summary', () => {
  const s = (through: string) => summary(through)

  it('marks where a summary ends and hides nothing', () => {
    const rows = [user('q1'), assistant('a1', id('r1')), user('q2'), assistant('a2', id('r2'))]
    const turns = buildTurns(rows, null, undefined, s('a1'))
    expect(turns.map((t) => t.kind)).toEqual(['user', 'assistant', 'user', 'assistant'])
    expect(turns.map((t) => Boolean(t.after))).toEqual([false, true, false, false])
    expect(turns[1]).toMatchObject({ after: { throughMessageId: id('a1') } })
  })

  it('puts the divider after the user turn when the summary ends at a question', () => {
    // Compressed at the start of a run: the question is the last row, and kept.
    const turns = buildTurns([user('q1'), assistant('a1', id('r1')), user('q2')], null, undefined, s('q2'))
    expect(turns.map((t) => Boolean(t.after))).toEqual([false, false, true])
  })

  it('puts it after the reply that holds the last row, when it is the last thing it said', () => {
    const rows = [user('q'), toolCallRow('c', id('r1'), 'call-1'), toolResultRow('t', id('r1'), 'call-1')]
    const [, reply] = buildTurns(rows, null, undefined, s('t'))
    expect(reply).toMatchObject({ kind: 'assistant', after: { throughMessageId: id('t') } })
    expect((reply as AssistantTurn).inside).toBeUndefined()
  })

  it('puts it among the parts of a reply that went on after the compression', () => {
    // A compression between two tool rounds: the last row covered is a result
    // in the middle of the reply.
    const rows = [
      user('q'),
      toolCallRow('c1', id('r1'), 'call-1'),
      toolResultRow('t1', id('r1'), 'call-1'),
      toolCallRow('c2', id('r1'), 'call-2'),
      toolResultRow('t2', id('r1'), 'call-2'),
      assistant('final', id('r1')),
    ]
    const turns = buildTurns(rows, null, undefined, s('t1'))
    const reply = assistantTurns(turns)[0]!
    expect(reply.after).toBeUndefined()
    // After the first response, the call that the result answered.
    expect(reply.inside).toMatchObject({ index: 1, summary: { throughMessageId: id('t1') } })
  })

  it('keeps the divider above what a running reply says next', () => {
    const r = run('r1')
    const q = user('q')
    const call = toolCallRow('c', r.id, 'call-1')
    const result = toolResultRow('t', r.id, 'call-1')
    const turns = buildTurns([q, call, result], liveRun(r, [q, call, result], { partial: [text('streaming')] }), undefined, s('t'))
    const reply = assistantTurns(turns)[0]!
    expect(reply.after).toBeUndefined()
    expect(reply.inside).toMatchObject({ index: 1 })
  })

  it('draws no divider when the row it ends at is not among the rows', () => {
    const turns = buildTurns([user('q1'), assistant('a1', id('r1'))], null, undefined, s('gone'))
    expect(turns.some((t) => t.after || (t.kind === 'assistant' && t.inside))).toBe(false)
  })

  it('finds the row among the live run’s own', () => {
    const r = run('r1')
    const q = user('q')
    const answer = assistant('a', r.id)
    const turns = buildTurns([q], liveRun(r, [q, answer]), undefined, s('q'))
    expect(turns[0]).toMatchObject({ kind: 'user', after: { throughMessageId: id('q') } })
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

// A message content moderation refused is a user row of status `blocked`: drawn
// as the person's message followed by the reply that says so, wherever it falls
// in the transcript, and no part of what was asked and answered.
describe('a refused message', () => {
  const refused = (name: string) => user(name, { status: 'blocked' })
  const kinds = (turns: ReturnType<typeof buildTurns>) => turns.map((t) => t.kind)

  it('is the person’s message followed by its reply card', () => {
    const turns = buildTurns([user('q'), assistant('a', id('r1')), refused('bad')], null)
    expect(kinds(turns)).toEqual(['user', 'assistant', 'user', 'refusal'])
    expect(turns[2]).toMatchObject({ kind: 'user', key: id('bad') })
    expect(turns[3]).toMatchObject({ kind: 'refusal', message: { id: id('bad'), content: 'bad' } })
    expect(isRefusedTurn(turns[2]!)).toBe(true)
    expect(isRefusedTurn(turns[3]!)).toBe(true)
    expect(isRefusedTurn(turns[0]!)).toBe(false)
    expect(sentTurns(turns).map((t) => t.kind)).toEqual(['user', 'assistant'])
  })

  it('is a transcript of its own in a conversation that has nothing else', () => {
    expect(kinds(buildTurns([refused('bad')], null))).toEqual(['user', 'refusal'])
    // Two in a row, and a question after: each has its card.
    expect(kinds(buildTurns([refused('b1'), refused('b2'), user('q')], null))).toEqual(['user', 'refusal', 'user', 'refusal', 'user'])
  })

  it('does not end the reply before it: the rows after a question the model asked still join its block', () => {
    const ask = { ...toolCallRow('ask', id('r1'), 'call-ask', ASK_USER), stopReason: 'tool_calls' as const }
    const answer = toolResultRow('ans', id('r1'), 'call-ask', ASK_USER)
    const after = assistant('after', id('r2'))
    const turns = buildTurns([user('q'), ask, refused('bad'), answer, after], null)
    // One reply, the one that asked and went on; the refused message beside it.
    expect(assistantTurns(turns)).toHaveLength(1)
    expect(assistantTurns(turns)[0]!.responses.map((m) => m.id)).toEqual([ask.id, after.id])
    expect(sentTurns(turns).map((t) => t.kind)).toEqual(['user', 'assistant'])
  })

  it('goes with the answer being made again', () => {
    const q = user('q')
    const rows = [q, assistant('a', id('r1')), refused('bad')]
    // The click hides what is after the question, the refused message included.
    expect(kinds(buildTurns(rows, null, { after: q.id }))).toEqual(['user'])
    // The run that the server started is on the question; the old rows after it are not shown.
    const r2 = run('r2')
    expect(kinds(buildTurns(rows, liveRun(r2, [q]), undefined))).toEqual(['user', 'assistant'])
  })

  it('goes with an edit that reaches it, and is itself what an edit of it hides', () => {
    const bad = refused('bad')
    const rows = [user('q1'), assistant('a1', id('r1')), bad, user('q2')]
    expect(kinds(buildTurns(rows, null, { from: bad.id }))).toEqual(['user', 'assistant'])
  })

  it('leaves the running reply where a run puts it, after the card', () => {
    const r2 = run('r2')
    const turns = buildTurns([user('q1'), assistant('a1', id('r1')), refused('bad'), user('q2')], liveRun(r2, [user('q2')]))
    expect(turns.at(-1)).toMatchObject({ kind: 'assistant', key: r2.id })
  })

  it('does not hide the question the model is waiting on, and is not the reply to it', () => {
    const rows = [
      user('q'),
      { ...toolCallRow('ask', id('r1'), 'call-ask', ASK_USER), stopReason: 'tool_calls' as const },
      refused('bad'),
    ]
    const turns = buildTurns(rows, null)
    // Not answerable while the refused message ends the conversation: it is reworded first.
    expect(pendingQuestion(turns, false)).toBeNull()
    // With something after the refused message (old data), the question is still the question.
    const after = [...rows, assistant('stray', id('r1'))]
    expect(refusedTail(buildTurns(after, null))).toBeNull()
  })
})

describe('refusedTail', () => {
  const refused = (name: string) => user(name, { status: 'blocked' })

  it('is the refused message the conversation ends on', () => {
    const bad = refused('bad')
    expect(refusedTail(buildTurns([user('q'), assistant('a', id('r1')), bad], null))).toBe(bad)
    expect(refusedTail(buildTurns([bad], null))).toBe(bad)
  })

  it('is nothing when anything follows it, or nothing was refused', () => {
    expect(refusedTail(buildTurns([user('q'), assistant('a', id('r1'))], null))).toBeNull()
    expect(refusedTail(buildTurns([refused('bad'), user('q')], null))).toBeNull()
    expect(refusedTail(buildTurns([], null))).toBeNull()
  })

  it('is gone while a redo hides it, and while a run follows it', () => {
    const bad = refused('bad')
    const rows = [user('q'), assistant('a', id('r1')), bad]
    expect(refusedTail(buildTurns(rows, null, { from: bad.id }))).toBeNull()
    expect(refusedTail(buildTurns(rows, liveRun(run('r2'), [user('q2')])))).toBeNull()
  })
})
