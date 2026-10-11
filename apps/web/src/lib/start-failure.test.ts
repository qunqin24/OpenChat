import { OpenChatError } from '@openchat/api-client'
import { describe, expect, it } from 'vitest'

import { returnableFiles, startWroteNothing } from './start-failure'

const error = (status: number, code: string, details?: Record<string, unknown>) =>
  new OpenChatError(status, { code, message: 'x', ...(details ? { details } : {}) })

describe('startWroteNothing', () => {
  it('is true for a refusal that writes nothing', () => {
    expect(startWroteNothing(error(429, 'too_many_runs'))).toBe(true)
    expect(startWroteNothing(error(409, 'no_models'))).toBe(true)
    // A content refusal of an edit or answer keeps nothing, but a first send always names its row.
    expect(startWroteNothing(error(422, 'content_blocked'))).toBe(true)
  })

  it('is false for a refused message the server kept in the conversation', () => {
    expect(startWroteNothing(error(422, 'content_blocked', { messageId: 'm-1' }))).toBe(false)
  })

  it('is false when the message may have been written or the page is signed out', () => {
    expect(startWroteNothing(error(500, 'internal'))).toBe(false)
    expect(startWroteNothing(error(401, 'unauthorized'))).toBe(false)
    expect(startWroteNothing(new Error('network'))).toBe(false)
  })
})

describe('returnableFiles', () => {
  const files = [{ id: 'a' }, { id: 'b' }]

  it('gives the uploads back, so the person need not add them again', () => {
    expect(returnableFiles(error(429, 'too_many_runs'), files)).toEqual(files)
    expect(returnableFiles(error(422, 'content_blocked', { messageId: 'm-1' }), files)).toEqual(files)
    expect(returnableFiles(new Error('network'), files)).toEqual(files)
  })

  it('does not give back files the server cannot use', () => {
    expect(returnableFiles(error(422, 'attachment_unavailable'), files)).toEqual([])
  })
})
