import { describe, expect, it } from 'vitest'

import { answerQuote, parseQuote, parseQuotes, passageFor, quoteMessage } from './document-selection'

const doc = '# Pulse\n\n**Pulse** 是一款原生工具。\n\n它的核心特点：\n\n- 统一监控\n- 原生体验\n\nPulse 是一款原生工具。'

describe('passageFor', () => {
  it('takes the selection when it occurs once', () => {
    expect(passageFor(doc, ['它的核心特点：', 'whatever'])).toBe('它的核心特点：')
  })

  it('falls back to the whole blocks when the selection repeats or is not in the source', () => {
    // "是一款原生工具。" is in two paragraphs; the first block is unique.
    expect(passageFor(doc, ['是一款原生工具。', '**Pulse** 是一款原生工具。'])).toBe('**Pulse** 是一款原生工具。')
    expect(passageFor(doc, ['not there', '- 统一监控\n- 原生体验'])).toBe('- 统一监控\n- 原生体验')
  })

  it('is null when nothing is unique', () => {
    expect(passageFor(doc, ['Pulse', ''])).toBeNull()
  })
})

describe('quoteMessage / parseQuote', () => {
  it('round-trips', () => {
    const quote = { documentId: 'd1', title: 'Pulse "介绍"', passage: '它的核心特点：\n\n- 统一监控' }
    const message = quoteMessage(quote, '改得更有吸引力')
    expect(parseQuote(message)).toEqual({ quote: { ...quote, title: 'Pulse  介绍 ' }, request: '改得更有吸引力' })
  })

  it('round-trips a passage of an answer', () => {
    const message = quoteMessage(answerQuote('  先用高德就行。\n个人开发阶段够用。 '), '为什么不用百度？')
    expect(message).toBe('<quote>\n先用高德就行。\n个人开发阶段够用。\n</quote>\n\n为什么不用百度？')
    expect(parseQuote(message)).toEqual({ quote: answerQuote('先用高德就行。\n个人开发阶段够用。'), request: '为什么不用百度？' })
  })

  it('round-trips several passages of answers, in order', () => {
    const quotes = [answerQuote('第一段'), answerQuote('第二段\n两行')]
    const message = quoteMessage(quotes, '这两段矛盾吗？')
    expect(parseQuotes(message)).toEqual({ quotes, request: '这两段矛盾吗？' })
  })

  it('leaves ordinary messages alone', () => {
    expect(parseQuote('hello <document-selection>')).toBeNull()
    expect(parseQuote('hello <quote>\nx\n</quote>')).toBeNull()
    expect(parseQuotes('hello')).toBeNull()
  })
})
