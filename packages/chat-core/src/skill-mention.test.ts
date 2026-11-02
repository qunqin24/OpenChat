import { describe, expect, it } from 'vitest'

import { mentionAt, parseSkills, skillMessage } from './skill-mention'

describe('skill mentions', () => {
  it('puts a line per skill before the message, and reads them back', () => {
    const content = skillMessage(['weekly-report', 'house-style', 'weekly-report'], '写周报')
    expect(content).toBe('<skill name="weekly-report"/>\n<skill name="house-style"/>\n写周报')
    expect(parseSkills(content)).toEqual({ names: ['weekly-report', 'house-style'], rest: '写周报' })
  })

  it('leaves a message without them, and tags later in the text, alone', () => {
    expect(parseSkills('hello')).toEqual({ names: [], rest: 'hello' })
    expect(parseSkills('see <skill name="x"/>\n')).toEqual({ names: [], rest: 'see <skill name="x"/>\n' })
  })

  it('finds the $name being typed at the caret', () => {
    expect(mentionAt('$wee', 4)).toEqual({ query: 'wee', start: 0 })
    expect(mentionAt('写个 $Wee', 7)).toEqual({ query: 'wee', start: 3 })
    expect(mentionAt('price$5', 7)).toBeNull()
    expect(mentionAt('$week report', 12)).toBeNull()
  })
})
