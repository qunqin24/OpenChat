import { OpenChatError } from '@openchat/api-client'
import { describe, expect, it } from 'vitest'

import { refusalOf, saveRefusalOf } from './moderation-refusal'

const error = (code: string, details?: Record<string, unknown>) =>
  new OpenChatError(422, { code, message: 'x', ...(details ? { details } : {}) })

describe('saveRefusalOf', () => {
  it('is a refusal with its warning for content_blocked', () => {
    expect(saveRefusalOf(error('content_blocked', { categories: ['violence'], strikesLeft: 2, windowHours: 24 }))).toEqual({
      kind: 'blocked',
      strikesLeft: 2,
      windowHours: 24,
    })
    expect(saveRefusalOf(error('content_blocked', { categories: ['violence'] }))).toEqual({ kind: 'blocked' })
  })

  it('is unavailable when the check could not be made', () => {
    expect(saveRefusalOf(new OpenChatError(503, { code: 'moderation_unavailable', message: 'x' }))).toEqual({ kind: 'unavailable' })
  })

  it('is null for every other failure', () => {
    expect(saveRefusalOf(error('memory_full'))).toBeNull()
    expect(saveRefusalOf(new Error('boom'))).toBeNull()
  })
})

describe('refusalOf', () => {
  it('is null for anything but content_blocked', () => {
    expect(refusalOf(error('too_many_runs'))).toBeNull()
    expect(refusalOf(new Error('boom'))).toBeNull()
  })

  it('carries the strike warning when the server gives one', () => {
    expect(refusalOf(error('content_blocked', { categories: ['violence'], strikesLeft: 2, windowHours: 24 }))).toEqual({
      strikesLeft: 2,
      windowHours: 24,
    })
  })

  it('has no warning when none will follow, or the numbers make no sense', () => {
    expect(refusalOf(error('content_blocked', { categories: ['violence'] }))).toEqual({})
    expect(refusalOf(error('content_blocked', { strikesLeft: 0, windowHours: 24 }))).toEqual({})
    expect(refusalOf(error('content_blocked', { strikesLeft: '2', windowHours: 24 }))).toEqual({})
  })

  it('names the message the server kept, when it kept one', () => {
    expect(refusalOf(error('content_blocked', { categories: ['violence'], messageId: 'm-1', strikesLeft: 1, windowHours: 24 }))).toEqual({
      messageId: 'm-1',
      strikesLeft: 1,
      windowHours: 24,
    })
    // An edit or an answer that is refused keeps nothing and names nothing; nor does a wrong type pass.
    expect(refusalOf(error('content_blocked', { messageId: 7 }))).toEqual({})
    expect(refusalOf(error('content_blocked', { messageId: '' }))).toEqual({})
  })
})
