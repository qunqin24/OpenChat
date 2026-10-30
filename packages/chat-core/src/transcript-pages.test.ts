import type { Message } from '@openchat/api-client'
import { describe, expect, it } from 'vitest'

import { assistant, id, user } from './testing'
import { appendRow, emptyTranscript, spliceNewest, truncateTranscript, type TranscriptPages } from './transcript-pages'

/** Pages as the cache holds them: newest first, each page's items oldest first. */
const pages = (...held: { items: Message[]; olderCursor?: string | null }[]): TranscriptPages => ({
  pages: held.map((p) => ({ items: p.items, olderCursor: p.olderCursor ?? null })),
  pageParams: held.map((_, i) => (i === 0 ? undefined : `cursor-${i}`)),
})

const ids = (held: TranscriptPages | undefined) =>
  held?.pages
    .slice()
    .reverse()
    .flatMap((p) => p.items.map((m) => m.id))

describe('spliceNewest', () => {
  const rows = Array.from({ length: 8 }, (_, i) => (i % 2 === 0 ? user(`q${i}`) : assistant(`a${i}`, id(`r${i}`))))

  it('keeps what is older than the fresh page and takes the fresh page for the rest', () => {
    const held = pages({ items: rows.slice(4) }, { items: rows.slice(0, 4), olderCursor: 'older' })
    const fresh = { items: rows.slice(4, 6), olderCursor: 'x' }
    const next = spliceNewest(held, fresh)
    expect(ids(next)).toEqual(rows.slice(0, 6).map((m) => m.id))
    expect(next.pages[0]).toBe(fresh)
  })

  it('drops rows that were deleted since: they are not in the fresh page', () => {
    // An edit of row 5 deleted it and the rows after it, and wrote a new question.
    const held = pages({ items: rows })
    const reworded = user('q5b')
    const fresh = { items: [...rows.slice(2, 5), reworded, assistant('a5b', id('r9'))], olderCursor: 'x' }
    expect(ids(spliceNewest(held, fresh))).toEqual([...rows.slice(0, 5).map((m) => m.id), reworded.id, id('a5b')])
  })

  it('takes the fresh page whole when it starts at a row that is not held', () => {
    // The edit reached back past everything held: what is held is all gone.
    const held = pages({ items: rows.slice(4) })
    const fresh = { items: [user('new'), assistant('newer', id('r9'))], olderCursor: null }
    expect(ids(spliceNewest(held, fresh))).toEqual([id('new'), id('newer')])
  })

  it('takes the fresh page when nothing is held', () => {
    const fresh = { items: rows.slice(0, 2), olderCursor: null }
    expect(spliceNewest(undefined, fresh).pages).toEqual([fresh])
  })
})

describe('truncateTranscript', () => {
  const q1 = user('q1')
  const a1 = assistant('a1', id('r1'))
  const q2 = user('q2')
  const a2 = assistant('a2', id('r2'))
  const held = () => pages({ items: [q2, a2] }, { items: [q1, a1], olderCursor: 'older' })

  it('drops the message named by `from` and everything after it', () => {
    const next = truncateTranscript(held(), { from: q2.id })
    expect(ids(next)).toEqual([q1.id, a1.id])
    // The way back to older rows is the oldest page's.
    expect(next?.pages[0]?.olderCursor).toBe('older')
  })

  it('keeps the message named by `after` and drops what follows it', () => {
    expect(ids(truncateTranscript(held(), { after: q2.id }))).toEqual([q1.id, a1.id, q2.id])
  })

  it('empties the transcript when the first message is the one edited', () => {
    expect(ids(truncateTranscript(held(), { from: q1.id }))).toEqual([])
  })

  it('leaves the rows as they are when the message is not among them', () => {
    // The cache has caught up already, or the message is older than anything loaded.
    const before = held()
    expect(truncateTranscript(before, { from: id('gone') })).toBe(before)
    expect(truncateTranscript(undefined, { from: q1.id })).toBeUndefined()
  })
})

describe('appendRow', () => {
  const q1 = user('q1')
  const a1 = assistant('a1', id('r1'))
  const kept = user('kept', { status: 'blocked' })

  it('puts the row at the end of the newest page, the older pages as they were', () => {
    const held = pages({ items: [a1] }, { items: [q1], olderCursor: 'older' })
    const next = appendRow(held, kept)!
    expect(ids(next)).toEqual([q1.id, a1.id, kept.id])
    expect(next.pages[1]).toBe(held.pages[1])
    expect(next.pageParams).toEqual(held.pageParams)
    // Nothing was changed in what was held.
    expect(ids(held)).toEqual([q1.id, a1.id])
  })

  it('does not add a row that is held already, in any page', () => {
    const held = pages({ items: [a1, kept] }, { items: [q1], olderCursor: null })
    expect(appendRow(held, kept)).toBe(held)
    expect(appendRow(pages({ items: [a1] }, { items: [kept] }), kept)).toBeDefined()
  })

  it('makes nothing of an empty cache: the fetch that fills it has the row', () => {
    expect(appendRow(undefined, kept)).toBeUndefined()
    expect(appendRow({ pages: [], pageParams: [] }, kept)).toEqual({ pages: [], pageParams: [] })
  })

  it('is replaced, not repeated, by the page fetched after it', () => {
    const held = appendRow(pages({ items: [q1, a1] }), kept)!
    const fresh = { items: [q1, a1, kept], olderCursor: null }
    expect(ids(spliceNewest(held, fresh))).toEqual([q1.id, a1.id, kept.id])
  })
})

describe('emptyTranscript', () => {
  it('holds a page with no rows, which a row can be added to and a fetch spliced over', () => {
    const empty = emptyTranscript()
    expect(ids(empty)).toEqual([])
    const row = user('hello')
    expect(ids(appendRow(empty, row))).toEqual([row.id])
    const fresh = { items: [row], olderCursor: null }
    expect(spliceNewest(empty, fresh).pages[0]).toBe(fresh)
  })
})
