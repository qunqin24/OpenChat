import { describe, expect, it } from 'vitest'

import { splitSegments } from './segments'

const fence = (body: string) => '```openchat-ui\n' + body + '```\n'

describe('splitSegments', () => {
  it('splits prose and blocks in order', () => {
    expect(splitSegments('Before\n' + fence('root = Text("x")\n') + 'After', false)).toEqual([
      { kind: 'markdown', text: 'Before\n' },
      { kind: 'ui', source: 'root = Text("x")\n', complete: true },
      { kind: 'markdown', text: '\nAfter' },
    ])
  })

  it('drops a block that was opened and closed with nothing in it', () => {
    const text = '## Rankings\n' + fence('\n') + '| a | b |\n| - | - |\n| 1 | 2 |\n'
    expect(splitSegments(text, false).map((s) => s.kind)).toEqual(['markdown', 'markdown'])
  })

  it('keeps an unclosed block while the answer streams, even before anything is in it', () => {
    expect(splitSegments('Intro\n```openchat-ui\n', true).at(-1)).toEqual({ kind: 'ui', source: '', complete: false })
  })
})
