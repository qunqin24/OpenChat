import { describe, expect, it } from 'vitest'

import { answerQuote, quoteMessage } from './document-selection'
import { imageRefMessage } from './image-ref'
import { skillMessage } from './skill-mention'
import { parseUserMessage, rewordUserMessage } from './user-message'

describe('parseUserMessage', () => {
  it('leaves an ordinary message as its words', () => {
    expect(parseUserMessage('你好')).toEqual({ research: false, skills: [], pictures: [], quotes: [], words: '你好' })
  })

  it('takes every layer off, in the order they are written', () => {
    const quote = answerQuote('先用高德就行。')
    const content = skillMessage(['pdf', 'deep-dive'], imageRefMessage(['img_0a1b2c3d'], quoteMessage(quote, '改得更短')), true)
    expect(parseUserMessage(content)).toEqual({
      research: true,
      skills: ['pdf', 'deep-dive'],
      pictures: ['img_0a1b2c3d'],
      quotes: [quote],
      words: '改得更短',
    })
  })

  it('reads a tag that is not at the start as words', () => {
    const content = '看这个：\n<skill name="pdf"/>\n'
    expect(parseUserMessage(content).skills).toEqual([])
    expect(parseUserMessage(content).words).toBe(content)
  })
})

describe('rewordUserMessage', () => {
  it('puts back what was picked around the new words', () => {
    const content = skillMessage(['pdf'], imageRefMessage(['img_0a1b2c3d'], quoteMessage(answerQuote('第一段'), '旧的')), true)
    const reworded = rewordUserMessage(parseUserMessage(content), '新的')
    expect(reworded).toBe(content.replace('旧的', '新的'))
    expect(parseUserMessage(reworded).words).toBe('新的')
  })

  it('is the words alone for an ordinary message', () => {
    expect(rewordUserMessage(parseUserMessage('旧的'), '新的')).toBe('新的')
  })
})
