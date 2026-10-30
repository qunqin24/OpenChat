import { describe, expect, it } from 'vitest'

import { plainText, turnOutline } from './outline'
import { answerQuote, quoteMessage } from './document-selection'
import { assistant, text, user } from './testing'
import { buildTurns } from './transcript'

describe('turnOutline', () => {
  it('pairs each thing the user said with the start of its answer', () => {
    const rows = [
      user('first', { content: 'What is\n  a monad?' }),
      assistant('a1', 'r1', { parts: [text('## A monad\n\nIs **a** [pattern](https://x.y).')] }),
      user('second', { content: 'And a functor?' }),
    ]
    expect(turnOutline(buildTurns(rows, null)).map(({ question, answer }) => ({ question, answer }))).toEqual([
      { question: 'What is a monad?', answer: 'A monad Is a pattern.' },
      { question: 'And a functor?', answer: '' },
    ])
  })

  it('shows the words of a request about a quoted passage, and the files of one without words', () => {
    const quoted = quoteMessage(answerQuote('the passage'), 'shorter please')
    const rows = [
      user('q', { content: quoted }),
      user('f', { content: '', attachments: [{ id: 'x', filename: 'plan.pdf', contentType: 'application/pdf', size: 1, url: 'u' }] }),
    ]
    expect(turnOutline(buildTurns(rows, null)).map((entry) => entry.question)).toEqual(['shorter please', 'plan.pdf'])
  })
})

describe('plainText', () => {
  it('drops code, images and the marks of emphasis, lists, quotes and tables', () => {
    expect(plainText('- one\n- *two*\n> said\n```js\ncode()\n```\n![x](y.png) `z` $x$')).toBe('one two said z x')
    expect(plainText('| a | b |\n| --- | --- |\n| 1 | 2 |')).toBe('a b 1 2')
  })
})
