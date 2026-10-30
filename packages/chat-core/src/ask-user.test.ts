import type { ToolResultPart } from '@openchat/api-client'
import { describe, expect, it } from 'vitest'

import { answersOf, parseQuestions } from './ask-user'

describe('parseQuestions', () => {
  it('reads questions with their options', () => {
    expect(
      parseQuestions({
        questions: [
          {
            question: 'Which one?',
            header: 'Pick',
            multiSelect: true,
            options: [{ label: 'A', description: 'first' }, { label: 'B' }],
          },
        ],
      }),
    ).toEqual([
      {
        question: 'Which one?',
        header: 'Pick',
        multiSelect: true,
        options: [{ label: 'A', description: 'first' }, { label: 'B' }],
      },
    ])
  })

  it('defaults the header to nothing and multi-select to off, and drops empty descriptions', () => {
    expect(parseQuestions({ questions: [{ question: 'Q', options: [{ label: 'A', description: '' }] }] })).toEqual([
      { question: 'Q', header: '', multiSelect: false, options: [{ label: 'A' }] },
    ])
    // Only a real `true` selects several.
    const [question] = parseQuestions({ questions: [{ question: 'Q', multiSelect: 'yes', options: [] }] }) ?? []
    expect(question?.multiSelect).toBe(false)
  })

  it('is null while the arguments are still streaming', () => {
    expect(parseQuestions(undefined)).toBeNull()
    expect(parseQuestions({})).toBeNull()
    expect(parseQuestions({ questions: [] })).toBeNull()
  })

  it('is null for anything that is not a question', () => {
    expect(parseQuestions({ questions: 'nope' })).toBeNull()
    expect(parseQuestions({ questions: [null] })).toBeNull()
    expect(parseQuestions({ questions: [[]] })).toBeNull()
    expect(parseQuestions({ questions: [{ question: 1, options: [] }] })).toBeNull()
    expect(parseQuestions({ questions: [{ question: 'Q' }] })).toBeNull()
    expect(parseQuestions({ questions: [{ question: 'Q', options: [{ description: 'no label' }] }] })).toBeNull()
    // One bad question spoils the call, as it does on the server.
    expect(parseQuestions({ questions: [{ question: 'Q', options: [] }, { options: [] }] })).toBeNull()
  })
})

describe('answersOf', () => {
  const result = (over: Partial<ToolResultPart>): ToolResultPart => ({
    type: 'tool_result',
    callId: 'c',
    name: 'ask_user',
    content: '',
    status: 'succeeded',
    ...over,
  })

  it('reads the recorded answers by position', () => {
    expect(
      answersOf(result({ details: { answers: [{ selected: ['A', 'B'] }, { selected: [], other: 'something else' }] } })),
    ).toEqual([{ selected: ['A', 'B'] }, { selected: [], other: 'something else' }])
  })

  it('is null without a successful result carrying a list', () => {
    expect(answersOf(undefined)).toBeNull()
    expect(answersOf(result({ status: 'failed', details: { answers: [] } }))).toBeNull()
    expect(answersOf(result({}))).toBeNull()
    expect(answersOf(result({ details: { answers: 'x' } }))).toBeNull()
  })

  it('tolerates malformed entries instead of throwing', () => {
    expect(answersOf(result({ details: { answers: [null, { selected: ['A', 3, null] }, { selected: 'A', other: '' }] } }))).toEqual([
      { selected: [] },
      { selected: ['A'] },
      { selected: [] },
    ])
  })
})
