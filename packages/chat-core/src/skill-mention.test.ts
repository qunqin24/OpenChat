import { describe, expect, it } from 'vitest'

import { mentionAt, parseSkills, skillMessage } from './skill-mention'

describe('skill mentions', () => {
  it('puts a line per skill before the message, and reads them back', () => {
    const content = skillMessage(['weekly-report', 'house-style', 'weekly-report'], '写周报')
    expect(content).toBe('<skill name="weekly-report"/>\n<skill name="house-style"/>\n写周报')
    expect(parseSkills(content)).toEqual({ names: ['weekly-report', 'house-style'], rest: '写周报', research: false })
  })

  it('leaves a message without them, and tags later in the text, alone', () => {
    expect(parseSkills('hello')).toEqual({ names: [], rest: 'hello', research: false })
    expect(parseSkills('see <skill name="x"/>\n')).toEqual({ names: [], rest: 'see <skill name="x"/>\n', research: false })
  })

  it('finds the /… being typed at the caret', () => {
    expect(mentionAt('/wee', 4)).toEqual({ query: 'wee', start: 0 })
    expect(mentionAt('写个 /Wee', 7)).toEqual({ query: 'wee', start: 3 })
    expect(mentionAt('@文件', 3)).toBeNull()
    expect(mentionAt('see a/b', 7)).toBeNull()
    expect(mentionAt('mail a@b.c', 10)).toBeNull()
    expect(mentionAt('open https://x', 14)).toBeNull()
    expect(mentionAt('/week report', 12)).toBeNull()
  })
})

describe('deep research line', () => {
  it('leads the skills and comes back out', () => {
    const content = skillMessage(['pdf'], 'Compare X and Y', true)
    expect(content).toBe('<deep-research/>\n<skill name="pdf"/>\nCompare X and Y')
    expect(parseSkills(content)).toEqual({ names: ['pdf'], rest: 'Compare X and Y', research: true })
  })
  it('is only a first line', () => {
    expect(parseSkills('Compare <deep-research/>\nX').research).toBe(false)
    expect(parseSkills('<skill name="pdf"/>\n<deep-research/>\nX').research).toBe(false)
  })
})
