import { describe, expect, it } from 'vitest'

import { findPassage } from './find-passage'

describe('findPassage', () => {
  it('finds a passage across paragraphs, whatever the whitespace', () => {
    const text = '不过，这只能证明它提供了 JavaScript 执行能力。如果你问的是 OpenAI 的源代码。'
    const passage = '只能证明它提供了 JavaScript 执行能力。\n如果你问的是'
    const [start, end] = findPassage(text, passage)!
    expect(text.slice(start, end)).toBe('只能证明它提供了 JavaScript 执行能力。如果你问的是')
  })

  it('is null for a passage that is not there, or empty', () => {
    expect(findPassage('abc', 'abd')).toBeNull()
    expect(findPassage('abc', ' \n ')).toBeNull()
  })
})
