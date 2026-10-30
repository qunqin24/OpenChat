import { describe, expect, it } from 'vitest'

import type { ToolCallPart } from '@openchat/api-client'

import { blocksTouched, changedPassages, documentChangeOf, documentOpOf, streamingText, pendingEditOf, pendingTitle } from './document-tool'

describe('documentChangeOf', () => {
  it('reads a create or an update, and nothing else', () => {
    const ok = (details: Record<string, unknown>) => ({ type: 'tool_result' as const, callId: 'c', name: 'document', content: '', status: 'succeeded' as const, details })
    expect(documentChangeOf(ok({ op: 'create', documentId: 'd', version: 1, title: 'Email' }))).toEqual({ op: 'create', documentId: 'd', version: 1, title: 'Email' })
    expect(documentChangeOf(ok({ op: 'read', documentId: 'd', version: 1 }))).toBeNull()
    expect(documentChangeOf({ ...ok({ op: 'create', documentId: 'd' }), status: 'failed' })).toBeNull()
  })
})

describe('pendingTitle', () => {
  it('reads the title from arguments still streaming', () => {
    expect(pendingTitle({ type: 'tool_call', id: 'c', name: 'document', rawArguments: '{"op":"create","title":"给 Sam 的请假\\"邮件' })).toBe('给 Sam 的请假"邮件')
    expect(pendingTitle({ type: 'tool_call', id: 'c', name: 'document', rawArguments: '{"op":"cre' })).toBe('')
  })
})

describe('pendingEditOf', () => {
  const id = '6f1c9a52-1b8e-4a77-9a3e-2d1f0c7b9e11'
  it('reads the finds that have arrived whole', () => {
    const raw = `{"op":"update","id":"${id}","edits":[{"find":"春天它并不起眼。","replace":"春天里"},{"find":"夏天它替修`
    expect(pendingEditOf({ type: 'tool_call', id: 'c', name: 'document', rawArguments: raw })).toEqual({ documentId: id, finds: ['春天它并不起眼。'], rewrite: false })
  })
  it('knows a whole rewrite, and ignores what is not an update', () => {
    expect(pendingEditOf({ type: 'tool_call', id: 'c', name: 'document', rawArguments: `{"op":"update","id":"${id}","content":"新` })?.rewrite).toBe(true)
    expect(pendingEditOf({ type: 'tool_call', id: 'c', name: 'document', rawArguments: '{"op":"create","title":"x"' })).toBeNull()
  })
})

describe('blocksTouched', () => {
  it('finds the blocks a Markdown passage shows in', () => {
    const blocks = ['楼下的那棵银杏', '搬到这条巷子的第一年。', '春天它并不起眼。玉兰先开。', '统一监控：集中展示额度。']
    expect(blocksTouched(blocks, ['春天它并不起眼。玉兰先开。'])).toEqual([2])
    expect(blocksTouched(blocks, ['- **统一监控**：集中展示额度。\n\n搬到这条巷子的第一年。'])).toEqual([1, 3])
  })
})

describe('changedPassages', () => {
  const before = 'Dear Sam,\n\nI would like Friday off.\n\nThe handover is on Thursday.\n\nThanks,\nAlice'
  it('shows the paragraphs that changed, with what was left out between them', () => {
    const after = 'Dear Sam,\n\nI would like Friday off, for a wedding.\n\nThe handover is on Thursday.\n\nThanks,\nAlice'
    expect(changedPassages(before, after)).toEqual([true, 'I would like Friday off, for a wedding.', true])
  })
  it('shows the whole text where most of it changed, or nothing new can be shown', () => {
    const rewritten = 'Hi Sam,\n\nFriday off, please.\n\nHandover Thursday.\n\nThanks,\nAlice'
    expect(changedPassages(before, rewritten)).toEqual([rewritten])
    const cut = 'Dear Sam,\n\nI would like Friday off.\n\nThanks,\nAlice'
    expect(changedPassages(before, cut)).toEqual([cut])
    expect(changedPassages(undefined, cut)).toEqual([cut])
  })
})

describe('streamingText', () => {
  const call = (rawArguments: string) => ({ type: 'tool_call' as const, id: 'c', name: 'document', rawArguments })
  it('reads the content as far as it has streamed, an escape cut in half left out', () => {
    expect(streamingText(call('{"op":"create","title":"T","content":"第一段\\n\\n第二'))).toBe('第一段\n\n第二')
    expect(streamingText(call('{"op":"create","content":"a\\'))).toBe('a')
    expect(streamingText(call('{"op":"create","content":"a\\u4e'))).toBe('a')
    expect(streamingText(call('{"op":"create","content":"a\\\\'))).toBe('a\\')
    expect(streamingText(call('{"op":"create","title":"T"'))).toBe('')
  })
  it('reads the replacements of an update', () => {
    const raw = '{"op":"update","id":"x","edits":[{"find":"old","replace":"new one"},{"find":"b","replace":"and the'
    expect(streamingText(call(raw))).toBe('new one\n\nand the')
    expect(streamingText({ ...call(''), arguments: { op: 'update', edits: [{ find: 'a', replace: 'b' }] } })).toBe('b')
  })
})

describe('documentOpOf', () => {
  const call = (over: Partial<ToolCallPart>): ToolCallPart => ({ type: 'tool_call', id: 'c', name: 'document', ...over }) as ToolCallPart

  it('says what a call does once its arguments say, parsed or still streaming', () => {
    expect(documentOpOf(call({ arguments: { op: 'read', id: 'x' } }))).toBe('read')
    expect(documentOpOf(call({ rawArguments: '{"id":"x","op":"upd' }))).toBeUndefined()
    expect(documentOpOf(call({ rawArguments: '{"id":"x","op":"read"' }))).toBe('read')
  })

  it('does not know before then: not a write', () => {
    expect(documentOpOf(call({ rawArguments: '{"id":"x"' }))).toBeUndefined()
    expect(documentOpOf(call({}))).toBeUndefined()
  })
})
