import type { TraceRequest } from '@openchat/api-client'
import { describe, expect, it } from 'vitest'

import { traceFigures, traceRows, traceSpans, type TraceMessage } from './trace'

const system: TraceMessage = { role: 'system', parts: [{ type: 'text', text: 'You are OpenChat.' }] }
const user: TraceMessage = { role: 'user', parts: [{ type: 'text', text: '[Date: 2026-10-27]\n搜索今天的新闻' }] }
const call: TraceMessage = {
  role: 'assistant',
  parts: [
    { type: 'reasoning', text: 'I should search.' },
    { type: 'tool_call', id: 'c1', name: 'web_search', rawArguments: '{"query":"news"}' },
  ],
}
const result: TraceMessage = { role: 'tool', parts: [{ type: 'tool_result', callId: 'c1', name: 'web_search', content: 'results…', status: 'succeeded' }] }
const answer: TraceMessage = { role: 'assistant', parts: [{ type: 'text', text: '以下是今天的新闻' }] }

function request(over: Omit<Partial<TraceRequest>, 'response'> & { messages: TraceMessage[]; reply: TraceMessage }): TraceRequest {
  const { messages, reply, ...rest } = over
  return {
    id: crypto.randomUUID(),
    runId: 'run-1',
    kind: 'chat',
    status: 'ok',
    startedAt: '2026-10-27T08:00:00.000Z',
    durationMs: 1000,
    firstTokenMs: 400,
    modelId: 'm',
    servedModel: 'm',
    channelName: 'c',
    inputTokens: 100,
    outputTokens: 30,
    reasoningTokens: 0,
    cacheReadTokens: 300,
    cacheWriteTokens: 0,
    estimated: false,
    request: { messages, tools: [], options: {} },
    response: { message: reply },
    ...rest,
  }
}

const first = request({ messages: [system, user], reply: call })
const second = request({
  messages: [system, user, call, result],
  reply: answer,
  startedAt: '2026-10-27T08:00:03.000Z',
  durationMs: 2000,
})

describe('traceRows', () => {
  it('shows what each request added, then what came back', () => {
    expect(traceRows([first, second]).map((row) => [row.request, row.kind, row.name ?? '', row.text])).toEqual([
      [0, 'system', '', 'You are OpenChat.'],
      [0, 'user', '', '[Date: 2026-10-27]\n搜索今天的新闻'],
      [0, 'reasoning', '', 'I should search.'],
      [0, 'tool_call', 'web_search', '{"query":"news"}'],
      // The call is the first request's answer, not the second's input.
      [1, 'tool_result', 'web_search', 'results…'],
      [1, 'assistant', '', '以下是今天的新闻'],
    ])
  })

  it('shows the system prompt again only when it changed', () => {
    const changed = request({ messages: [{ role: 'system', parts: [{ type: 'text', text: 'Other.' }] }, user], reply: answer })
    const kinds = traceRows([first, changed]).filter((row) => row.kind === 'system').map((row) => row.text)
    expect(kinds).toEqual(['You are OpenChat.', 'Other.'])
  })
})

describe('traceSpans', () => {
  it('puts the gap after a request that called tools on the tool row', () => {
    const { spans, total } = traceSpans([first, second])
    expect(spans).toEqual([
      { request: 0, kind: 'model', start: 0, end: 1000, firstAt: 400, error: false },
      { request: 0, kind: 'tool', start: 1000, end: 3000 },
      { request: 1, kind: 'model', start: 3000, end: 5000, firstAt: 3400, error: false },
    ])
    expect(total).toBe(5000)
  })
})

describe('traceFigures', () => {
  it('reads throughput after the first token and the cache hit rate', () => {
    expect(traceFigures(first)).toEqual({ generationMs: 600, throughput: 50, promptTokens: 400, cacheHit: 0.75 })
  })
})
