import { describe, expect, it } from 'vitest'

import { processSpan, processThinking } from './reply-process'

describe('processSpan', () => {
  it('folds the thinking, the words said on the way and the calls into one, up to the answer', () => {
    expect(processSpan(['reasoning', 'text', 'tool', 'reasoning', 'tool', 'reasoning', 'text'])).toEqual({ start: 0, end: 5 })
    expect(processSpan(['reasoning', 'tool', 'tool', 'text'])).toEqual({ start: 0, end: 2 })
  })

  it('folds the thinking alone when no tool was called', () => {
    expect(processSpan(['reasoning', 'text'])).toEqual({ start: 0, end: 0 })
    expect(processSpan(['reasoning'])).toEqual({ start: 0, end: 0 })
  })

  it('has none when the reply only answered', () => {
    expect(processSpan(['text'])).toBeNull()
    expect(processSpan([null, 'other', 'text'])).toBeNull()
  })

  it('starts after what is drawn in the reply’s own flow, and passes over what draws nothing', () => {
    expect(processSpan(['other', null, 'reasoning', 'tool', null, 'reasoning', 'text'])).toEqual({ start: 2, end: 5 })
  })

  it('keeps the answer’s own thinking out of a process that ended before its words', () => {
    expect(processSpan(['reasoning', 'text', 'reasoning'])).toEqual({ start: 0, end: 0 })
  })
})

describe('processThinking', () => {
  const run = { running: true, compacting: false, retrying: false, toolRunning: false, partialEmpty: false, thoughtStreaming: false }

  it('says the model is thinking while its thought streams', () => {
    expect(processThinking(['reasoning'], { start: 0, end: 0 }, { ...run, thoughtStreaming: true })).toBe('reasoning')
  })

  it('waits between a step and the next response', () => {
    expect(processThinking(['tool'], { start: 0, end: 0 }, { ...run, partialEmpty: true })).toBe('waiting')
    expect(processThinking(['tool'], { start: 0, end: 0 }, { ...run, partialEmpty: true, toolRunning: true })).toBe(false)
  })

  it('is over once the model says something after it, or the run has ended', () => {
    expect(processThinking(['reasoning', 'text'], { start: 0, end: 0 }, { ...run, partialEmpty: true, retrying: true })).toBe(false)
    expect(processThinking(['reasoning'], { start: 0, end: 0 }, { ...run, running: false, thoughtStreaming: true })).toBe(false)
  })
})
