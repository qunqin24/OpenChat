import { describe, expect, it } from 'vitest'

import { parseBlock } from './parse'
import { resolveBlock } from './resolve'

const resolve = (source: string) => resolveBlock(parseBlock(source, true), true).root

describe('resolveBlock: Image', () => {
  it('reads a picture described by image', () => {
    const root = resolve('root = Image("laptop keyboard", caption: "键盘", aspect: "square")')
    expect(root?.props).toMatchObject({ image: 'laptop keyboard', caption: '键盘', aspect: 'square' })
  })

  it('keeps transcripts from before image drawing: src by name, and the old argument order', () => {
    expect(resolve('root = Image(src: "https://x.test/a.png", caption: "c")')?.props).toMatchObject({
      src: 'https://x.test/a.png',
      caption: 'c',
    })
    // Image(src, alt, caption, link) as it was: each argument where it was.
    expect(resolve('root = Image("https://x.test/a.png", "alt text", "the caption", "https://x.test/page")')?.props).toMatchObject({
      image: 'https://x.test/a.png',
      alt: 'alt text',
      caption: 'the caption',
      link: 'https://x.test/page',
    })
  })
})
