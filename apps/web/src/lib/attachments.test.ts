import { describe, expect, it } from 'vitest'

import { acceptAttribute, acceptsOf, kindOf, MAX_TEXT_BYTES, mediaTypeOf, rejectionOf, TEXT_TYPE } from './attachments'

const file = (name: string, type = '', size = 10) => {
  const f = new File(['x'], name, { type })
  Object.defineProperty(f, 'size', { value: size })
  return f
}

describe('text files', () => {
  it('are known by their name, whatever the browser calls them', () => {
    expect(mediaTypeOf(file('app.ts', 'video/mp2t'))).toBe(TEXT_TYPE)
    expect(mediaTypeOf(file('notes.md'))).toBe(TEXT_TYPE)
    expect(mediaTypeOf(file('data.csv', 'text/csv'))).toBe(TEXT_TYPE)
    expect(mediaTypeOf(file('Dockerfile'))).toBe(TEXT_TYPE)
    expect(mediaTypeOf(file('photo.png', 'image/png'))).toBe('image/png')
    expect(mediaTypeOf(file('report.docx', 'application/vnd.openxmlformats-officedocument.wordprocessingml.document'))).not.toBe(TEXT_TYPE)
    expect(kindOf(mediaTypeOf(file('main.go')))).toBe('text')
  })

  it('go to any model, up to their size', () => {
    const textOnly = acceptsOf({ inputModalities: [] })
    expect(rejectionOf(file('notes.txt'), textOnly)).toBeNull()
    expect(rejectionOf(file('notes.txt', '', MAX_TEXT_BYTES + 1), textOnly)).toBe('size-text')
    expect(rejectionOf(file('photo.png', 'image/png'), textOnly)).toBe('model-image')
    expect(acceptAttribute(textOnly)).toContain('.md')
  })
})
