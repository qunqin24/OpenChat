import type { BatchEvent, ContextSummary, Message, MessagePart, Run, RunEvent } from '@openchat/api-client'

import type { LiveRun } from './run-state'

// Builders for the tests of this directory. They fill in what a test does not
// care about, so each one says only what it is about.

let counter = 0
const next = () => ++counter
/** Stable, readable ids: `id('a')` is the same on every call. */
export const id = (name: string) => `00000000-0000-4000-8000-${name.padStart(12, '0')}`

export function message(over: Partial<Message> & Pick<Message, 'id' | 'role'>): Message {
  return {
    conversationId: id('conv'),
    content: '',
    parts: [],
    status: 'complete',
    createdAt: new Date(1_700_000_000_000 + next() * 1000).toISOString(),
    ...over,
  }
}

export const user = (name: string, over: Partial<Message> = {}) => message({ id: id(name), role: 'user', content: name, ...over })

export function assistant(name: string, runId: string, over: Partial<Message> = {}) {
  return message({
    id: id(name),
    role: 'assistant',
    runId,
    parts: [{ type: 'text', text: name }],
    stopReason: 'stop',
    ...over,
  })
}

export function toolCallRow(name: string, runId: string, callId: string, tool = 'web_search', args?: Record<string, unknown>) {
  return assistant(name, runId, {
    stopReason: 'tool_calls',
    parts: [{ type: 'tool_call', id: callId, name: tool, rawArguments: '{}', ...(args ? { arguments: args } : {}) }],
  })
}

export function toolResultRow(name: string, runId: string, callId: string, tool = 'web_search', details?: Record<string, unknown>) {
  return message({
    id: id(name),
    role: 'tool',
    runId,
    parts: [{ type: 'tool_result', callId, name: tool, content: 'ok', status: 'succeeded', ...(details ? { details } : {}) }],
  })
}

export function run(name: string, over: Partial<Run> = {}): Run {
  return {
    id: id(name),
    conversationId: id('conv'),
    status: 'running',
    model: 'm',
    version: 1,
    createdAt: '2026-10-10T00:00:00Z',
    ...over,
  }
}

/** A conversation's summary, through the message named `through`. */
export function summary(through: string, over: Partial<ContextSummary> = {}): ContextSummary {
  return {
    content: '- the story so far',
    throughMessageId: id(through),
    model: 'm',
    inputTokens: 10,
    outputTokens: 5,
    createdAt: '2026-10-10T00:00:00Z',
    ...over,
  }
}

export function liveRun(r: Run, messages: Message[], over: Partial<LiveRun> = {}): LiveRun {
  return { run: r, messages, partial: null, retry: null, tools: {}, compacting: false, ...over }
}

export function batch(r: Run, version: number, events: RunEvent[]): BatchEvent {
  return { type: 'batch', runId: r.id, version, events }
}

export const text = (value: string): MessagePart => ({ type: 'text', text: value })
