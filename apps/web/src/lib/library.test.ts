import { describe, expect, it } from 'vitest'

import type { Attachment, LibraryFile } from '@openchat/api-client'

import { markRemoved, withoutFile, type LibraryPages } from './library'
import { id, user } from './testing'
import type { TranscriptPages } from './transcript-pages'

const file = (name: string): Attachment => ({ id: id(name), filename: `${name}.png`, contentType: 'image/png', size: 4, url: `/f/${name}` })

const libraryFile = (name: string): LibraryFile => ({
  ...file(name),
  conversationId: id('conv'),
  conversationTitle: 'c',
  createdAt: new Date(0).toISOString(),
})

describe('markRemoved', () => {
  it('moves the file to the removed ones of the message it came with', () => {
    const held: TranscriptPages = {
      pages: [
        { items: [user('q2')], olderCursor: 'older' },
        { items: [user('q1', { attachments: [file('a'), file('b')] })], olderCursor: null },
      ],
      pageParams: [undefined, 'older'],
    }
    const next = markRemoved(held, id('a'))
    const message = next?.pages[1]?.items[0]
    expect(message?.attachments?.map((f) => f.filename)).toEqual(['b.png'])
    expect(message?.removedAttachments).toEqual([{ id: id('a'), filename: 'a.png', contentType: 'image/png' }])
    expect(next?.pages[0]?.items[0]).toBe(held.pages[0]?.items[0])
  })

  it('leaves a transcript without the file as it is', () => {
    const held: TranscriptPages = { pages: [{ items: [user('q1', { attachments: [file('b')] })], olderCursor: null }], pageParams: [undefined] }
    expect(markRemoved(held, id('a'))).toBe(held)
    expect(markRemoved(undefined, id('a'))).toBeUndefined()
  })
})

describe('withoutFile', () => {
  it('drops the file from whichever page holds it', () => {
    const held: LibraryPages = {
      pages: [{ items: [libraryFile('a'), libraryFile('b')], nextCursor: 'n' }, { items: [libraryFile('c')], nextCursor: null }],
      pageParams: [undefined, 'n'],
    }
    expect(withoutFile(held, id('b'))?.pages.map((page) => page.items.map((item) => item.filename))).toEqual([['a.png'], ['c.png']])
  })
})
