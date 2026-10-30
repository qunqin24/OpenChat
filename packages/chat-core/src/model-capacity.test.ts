import type { ChatModel } from '@openchat/api-client'
import { describe, expect, it } from 'vitest'

import { defaultModelId } from './model-capacity'

const model = (id: string, atCapacity = false): ChatModel => ({
  id,
  name: id,
  providerId: '',
  family: '',
  reasoning: false,
  toolCall: true,
  attachment: false,
  contextLimit: 0,
  outputLimit: 0,
  inputModalities: [],
  pdfAsText: false,
  reasoningEfforts: [],
  atCapacity,
})

describe('defaultModelId', () => {
  it('keeps the model last used while it has room', () => {
    expect(defaultModelId([model('a'), model('b')], 'b')).toBe('b')
  })

  it('steers around a last-used model that is at its cap', () => {
    expect(defaultModelId([model('a', true), model('b'), model('c')], 'a')).toBe('b')
  })

  it('takes the first with room when nothing was used', () => {
    expect(defaultModelId([model('a', true), model('b')])).toBe('b')
  })

  it('ignores a remembered model that is no longer offered', () => {
    expect(defaultModelId([model('a'), model('b')], 'gone')).toBe('a')
  })

  it('falls back to the first offered when every model is at its cap', () => {
    expect(defaultModelId([model('a', true), model('b', true)], 'b')).toBe('a')
  })

  it('is empty with no model offered', () => {
    expect(defaultModelId([])).toBe('')
  })
})
