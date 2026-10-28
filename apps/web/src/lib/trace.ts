import type { TraceRequest } from '@openchat/api-client'

/**
 * The 轨迹 view (TraceView): an administrator's own conversation as the model
 * saw it, request by request. The server keeps each request as sent and what
 * came back (api/traces.go) in llm's own encoding - `{role, parts}` messages -
 * which these read loosely: it is a debugging aid, not a contract.
 */

export interface TracePart {
  type: string
  id?: string
  text?: string
  name?: string
  arguments?: unknown
  rawArguments?: string
  callId?: string
  content?: string
  status?: string
  error?: string
  filename?: string
  mediaType?: string
}

export interface TraceMessage {
  role: string
  parts: TracePart[]
}

export type TraceRowKind = 'system' | 'user' | 'history' | 'reasoning' | 'assistant' | 'tool_call' | 'tool_result'

export interface TraceRow {
  key: string
  /** Which request, by index: what selecting the row selects. */
  request: number
  kind: TraceRowKind
  /** A tool's name, for calls and results. */
  name?: string
  text: string
  error?: boolean
}

export function messagesOf(request: TraceRequest): TraceMessage[] {
  const messages = (request.request as { messages?: unknown }).messages
  return Array.isArray(messages) ? (messages as TraceMessage[]) : []
}

export function responseOf(request: TraceRequest): TraceMessage | null {
  const message = (request.response as { message?: unknown }).message
  return message && typeof message === 'object' ? (message as TraceMessage) : null
}

export function toolsOf(request: TraceRequest): { name: string; description?: string }[] {
  const tools = (request.request as { tools?: unknown }).tools
  return Array.isArray(tools) ? (tools as { name: string; description?: string }[]) : []
}

export function systemOf(request: TraceRequest): string {
  return messagesOf(request)
    .filter((m) => m.role === 'system')
    .map((m) => textOf(m))
    .join('\n\n')
}

export function textOf(message: TraceMessage): string {
  return message.parts.filter((p) => p.type === 'text').map((p) => p.text ?? '').join('')
}

export function argumentsOf(part: TracePart): string {
  if (part.rawArguments) return part.rawArguments
  return part.arguments === undefined ? '' : JSON.stringify(part.arguments)
}

const same = (a: unknown, b: unknown) => JSON.stringify(a) === JSON.stringify(b)

/**
 * What each request added, in order: its system prompt when it changed, the
 * messages it carried that the one before did not (the turn's input, tool
 * results), then what came back. A request carries the whole history again;
 * only the new part is a row.
 */
export function traceRows(requests: TraceRequest[]): TraceRow[] {
  const rows: TraceRow[] = []
  let system = ''
  let previous: TraceMessage[] = []
  let previousResponse: TraceMessage | null = null

  requests.forEach((request, index) => {
    const push = (row: Omit<TraceRow, 'key' | 'request'>) => rows.push({ ...row, key: `${index}:${rows.length}`, request: index })
    const messages = messagesOf(request)

    const nextSystem = systemOf(request)
    if (nextSystem && nextSystem !== system) push({ kind: 'system', text: nextSystem })
    system = nextSystem

    const history = messages.filter((m) => m.role !== 'system')
    let common = 0
    while (common < history.length && common < previous.length && same(history[common], previous[common])) common++
    let added = history.slice(common)
    // The answer the request before got back comes first in this one's history.
    if (previousResponse && added[0]?.role === 'assistant' && same(added[0].parts, previousResponse.parts)) added = added.slice(1)
    for (const message of added) pushMessage(push, message, false)

    const response = responseOf(request)
    if (response) pushMessage(push, response, true)
    previous = response ? [...history, { role: 'assistant', parts: response.parts }] : history
    previousResponse = response
  })
  return rows
}

function pushMessage(push: (row: Omit<TraceRow, 'key' | 'request'>) => void, message: TraceMessage, answer: boolean) {
  const text = textOf(message)
  if (message.role === 'user') {
    // A user message starting with the date line is the turn's own input; the
    // server adds the line (agent.BuildContext).
    if (text || message.parts.some((p) => p.type === 'attachment')) {
      const files = message.parts.filter((p) => p.type === 'attachment').map((p) => `[${p.filename ?? 'file'}]`)
      push({ kind: 'user', text: [text, ...files].filter(Boolean).join(' ') })
    }
    for (const part of message.parts.filter((p) => p.type === 'tool_result')) pushResult(push, part)
    return
  }
  if (message.role === 'tool') {
    for (const part of message.parts) if (part.type === 'tool_result') pushResult(push, part)
    return
  }
  for (const part of message.parts) {
    if (part.type === 'reasoning' && part.text) push({ kind: 'reasoning', text: part.text })
    // An answer already in the history (one from before recording began, or a
    // compressed context's) is history, not what this request got back.
    else if (part.type === 'text' && part.text) push({ kind: answer ? 'assistant' : 'history', text: part.text })
    else if (part.type === 'tool_call') push({ kind: 'tool_call', name: part.name, text: argumentsOf(part) })
    else if (part.type === 'tool_result') pushResult(push, part)
  }
}

function pushResult(push: (row: Omit<TraceRow, 'key' | 'request'>) => void, part: TracePart) {
  const failed = Boolean(part.error) || (part.status !== undefined && part.status !== 'succeeded')
  push({ kind: 'tool_result', name: part.name, text: part.error || part.content || part.status || '', error: failed })
}

/** Figures for one request. */
export function traceFigures(request: TraceRequest) {
  const first = request.firstTokenMs ?? null
  const generationMs = first === null ? null : Math.max(0, request.durationMs - first)
  const prompt = request.inputTokens + request.cacheReadTokens + request.cacheWriteTokens
  return {
    generationMs,
    // Output per second of generation, after the first token.
    throughput: generationMs && generationMs > 0 ? (request.outputTokens * 1000) / generationMs : null,
    promptTokens: prompt,
    cacheHit: prompt > 0 ? request.cacheReadTokens / prompt : null,
  }
}

export interface TraceSpan {
  request: number
  kind: 'model' | 'tool'
  start: number
  end: number
  /** Where the first token came, for a model span: the wait before it is drawn lighter. */
  firstAt?: number
  error?: boolean
}

/**
 * The timeline: each request as a span on the model row, and the time from a
 * request that called tools to the next request of the same run on the tool
 * row (tool time is not recorded on its own; the gap is when they ran). In
 * milliseconds from the first request's start.
 */
export function traceSpans(requests: TraceRequest[]): { spans: TraceSpan[]; total: number } {
  if (requests.length === 0) return { spans: [], total: 0 }
  const origin = Date.parse(requests[0]!.startedAt)
  const spans: TraceSpan[] = []
  let total = 0
  requests.forEach((request, index) => {
    const start = Date.parse(request.startedAt) - origin
    const end = start + request.durationMs
    total = Math.max(total, end)
    spans.push({
      request: index,
      kind: 'model',
      start,
      end,
      ...(request.firstTokenMs != null ? { firstAt: start + request.firstTokenMs } : {}),
      error: request.status === 'error',
    })
    const next = requests[index + 1]
    const called = responseOf(request)?.parts.some((p) => p.type === 'tool_call')
    if (called && next && next.runId === request.runId) {
      const nextStart = Date.parse(next.startedAt) - origin
      if (nextStart > end) spans.push({ request: index, kind: 'tool', start: end, end: nextStart })
    }
  })
  return { spans, total }
}
