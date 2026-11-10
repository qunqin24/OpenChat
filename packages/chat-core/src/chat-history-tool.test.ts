import { describe, expect, it } from 'vitest'

import { historyHitsOf, historyKeywords, historyStepLabel } from './chat-history-tool'

describe('historyKeywords', () => {
  it('keeps the words, trimmed, and drops what is not one', () => {
    expect(historyKeywords({ keywords: [' go ', '', 3, 'postgres'] })).toEqual(['go', 'postgres'])
    expect(historyKeywords({})).toEqual([])
  })
})

describe('historyHitsOf', () => {
  it('reads a search\'s hits, with or without a title', () => {
    const result = {
      status: 'succeeded' as const,
      details: { op: 'search', keywords: ['a'], hits: [{ conversationId: 'c1', title: 'Trip', at: '2026-09-01T10:00:00Z' }, { conversationId: 'c2' }, { title: 'x' }, null] },
    }
    expect(historyHitsOf(result)).toEqual([{ conversationId: 'c1', title: 'Trip', at: '2026-09-01T10:00:00Z' }, { conversationId: 'c2' }])
    expect(historyHitsOf({ status: 'succeeded', details: { op: 'search', hits: [] } })).toEqual([])
  })
  it('reads the one conversation a read opened', () => {
    expect(historyHitsOf({ status: 'succeeded', details: { op: 'read', conversationId: 'c1', title: 'Trip' } })).toEqual([{ conversationId: 'c1', title: 'Trip' }])
    expect(historyHitsOf({ status: 'succeeded', details: { op: 'read', conversationId: 'c1' } })).toEqual([{ conversationId: 'c1' }])
  })
  it('has none for a call that failed or is still going', () => {
    expect(historyHitsOf({ status: 'failed', details: { op: 'read', conversationId: 'c1' } })).toEqual([])
    expect(historyHitsOf(undefined)).toEqual([])
  })
})

describe('historyStepLabel', () => {
  const t = ((key: string, o?: { query?: string }) => (o?.query ? `${key}|${o.query}` : key)) as never
  it('says what it does, with the keywords of a search', () => {
    expect(historyStepLabel(t, { op: 'search', keywords: ['a', 'b'] }, true)).toBe('chat.tool.history-searching-for|a b')
    expect(historyStepLabel(t, { op: 'search', keywords: ['a'] }, false)).toBe('chat.tool.history-search-for|a')
    expect(historyStepLabel(t, { op: 'search' }, true)).toBe('chat.tool.history-searching')
    expect(historyStepLabel(t, { op: 'read', conversation: 'c1' }, false)).toBe('chat.tool.history-read')
    expect(historyStepLabel(t, { op: 'read' }, true)).toBe('chat.tool.history-reading')
  })
})
