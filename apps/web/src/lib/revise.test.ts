import { describe, expect, it } from 'vitest'

import { ASK_USER } from './ask-user'
import { composerMode, reviseDraft } from './revise'
import { assistant, id, liveRun, run, toolCallRow, user } from './testing'
import { buildTurns, pendingQuestion, regenerable } from './transcript'

const refused = (name: string, content = name) => user(name, { status: 'blocked', content })

describe('the composer\'s mode', () => {
  it('sends, unless the conversation ends on a refused message', () => {
    expect(composerMode(buildTurns([], null))).toEqual({ kind: 'send' })
    expect(composerMode(buildTurns([user('q'), assistant('a', id('r1'))], null))).toEqual({ kind: 'send' })
    // A refused message with something after it is not where the conversation stops (old data).
    expect(composerMode(buildTurns([refused('bad'), user('q'), assistant('a', id('r1'))], null))).toEqual({ kind: 'send' })
  })

  it('revises the refused message the conversation ends on, alone or after an exchange', () => {
    const bad = refused('bad')
    expect(composerMode(buildTurns([bad], null))).toEqual({ kind: 'revise', message: bad })
    expect(composerMode(buildTurns([user('q'), assistant('a', id('r1')), bad], null))).toEqual({ kind: 'revise', message: bad })
  })

  it('is the last refused message of several, after an old one was left behind', () => {
    const [old, bad] = [refused('old'), refused('bad')]
    expect(composerMode(buildTurns([old, user('q'), assistant('a', id('r1')), bad], null))).toEqual({ kind: 'revise', message: bad })
  })

  it('sends while a redo hides the message, and while a run follows it', () => {
    const bad = refused('bad')
    const rows = [user('q'), assistant('a', id('r1')), bad]
    expect(composerMode(buildTurns(rows, null, { from: bad.id }))).toEqual({ kind: 'send' })
    expect(composerMode(buildTurns(rows, liveRun(run('r2'), [user('q2')])))).toEqual({ kind: 'send' })
  })
})

describe('what the revision box is filled with', () => {
  const bad = refused('bad', 'the words')
  const mode = composerMode(buildTurns([bad], null))

  it('takes the refused words when the box is empty - after a refresh, or on another device', () => {
    expect(reviseDraft(mode, '', null)).toEqual({ fill: 'the words', handled: bad.id })
    expect(reviseDraft(mode, '  \n', null)).toEqual({ fill: 'the words', handled: bad.id })
  })

  it('leaves what is in the box (refused from this box, the words are there already)', () => {
    expect(reviseDraft(mode, 'the words', null)).toEqual({ fill: null, handled: bad.id })
    expect(reviseDraft(mode, 'something else', null)).toEqual({ fill: null, handled: bad.id })
  })

  it('does not give the words back to a person who emptied the box', () => {
    expect(reviseDraft(mode, '', bad.id)).toEqual({ fill: null, handled: bad.id })
  })

  it('takes the words of a message refused again, which is another message', () => {
    const again = refused('again', 'new words')
    const next = composerMode(buildTurns([again], null))
    expect(reviseDraft(next, '', bad.id)).toEqual({ fill: 'new words', handled: again.id })
  })

  it('does nothing when sending', () => {
    expect(reviseDraft({ kind: 'send' }, '', bad.id)).toEqual({ fill: null, handled: bad.id })
  })
})

describe('what is offered while a refused message ends the conversation', () => {
  const answered = [user('q'), assistant('a', id('r1'))]

  it('redoes the last answer, until a refused message follows it', () => {
    expect(regenerable(buildTurns(answered, null))).toMatchObject({ inputId: id('q') })
    expect(regenerable(buildTurns([...answered, refused('bad')], null))).toBeNull()
  })

  it('redoes the last answer past an old refused message with a question after it', () => {
    expect(regenerable(buildTurns([refused('old'), ...answered], null))).toMatchObject({ inputId: id('q') })
  })

  it('does not ask the model\'s question again past the refused message', () => {
    const ask = { ...toolCallRow('ask', id('r1'), 'call-ask', ASK_USER), stopReason: 'tool_calls' as const }
    expect(pendingQuestion(buildTurns([user('q'), ask], null), false)).toBe('call-ask')
    expect(pendingQuestion(buildTurns([user('q'), ask, refused('bad')], null), false)).toBeNull()
  })
})
