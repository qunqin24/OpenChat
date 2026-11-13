import { describe, expect, it } from 'vitest'

import { MAX_MODEL_MARKS, missingModels } from './model-marks'

describe('missingModels', () => {
  it('waits for the list', () => {
    expect(missingModels(['a'], undefined)).toEqual([])
  })

  it('keeps the unlisted ones once each, sorted', () => {
    expect(missingModels(['b', null, 'kept', 'a', 'b', undefined, ''], new Set(['kept']))).toEqual(['a', 'b'])
  })

  it('asks for at most one request’s worth', () => {
    const ids = Array.from({ length: MAX_MODEL_MARKS + 5 }, (_, i) => `m${String(i).padStart(3, '0')}`)
    expect(missingModels(ids, new Set())).toHaveLength(MAX_MODEL_MARKS)
  })
})
