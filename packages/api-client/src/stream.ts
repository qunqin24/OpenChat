import { OpenChatError } from './errors'
import type { CreateMessageRequest, StreamEvent } from './types'

export interface StreamMessageOptions {
  baseUrl: string
  conversationId: string
  body: CreateMessageRequest
  signal?: AbortSignal
  headers?: HeadersInit
}

/**
 * POSTs a message and yields the server's SSE frames as they arrive.
 *
 * openapi-fetch cannot model a streaming response body, so this is hand-written
 * against the same contract. Frames are `data: <json>` separated by a blank line.
 */
export async function* streamMessage(
  options: StreamMessageOptions,
): AsyncGenerator<StreamEvent, void, undefined> {
  const { baseUrl, conversationId, body, signal, headers } = options

  const response = await fetch(
    `${baseUrl.replace(/\/$/, '')}/conversations/${conversationId}/messages`,
    {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', Accept: 'text/event-stream', ...headers },
      body: JSON.stringify(body),
      signal,
    },
  )

  if (!response.ok) throw await OpenChatError.fromResponse(response)
  if (!response.body) {
    throw new OpenChatError(response.status, {
      code: 'no_stream_body',
      message: 'the response had no readable body',
    })
  }

  const reader = response.body.pipeThrough(new TextDecoderStream()).getReader()
  let buffer = ''

  try {
    while (true) {
      const { done, value } = await reader.read()
      if (done) break

      buffer += value
      let boundary = buffer.indexOf('\n\n')
      while (boundary !== -1) {
        const frame = buffer.slice(0, boundary)
        buffer = buffer.slice(boundary + 2)
        const event = parseFrame(frame)
        if (event) yield event
        boundary = buffer.indexOf('\n\n')
      }
    }

    // A stream that ends without a trailing blank line still has one last frame.
    const trailing = parseFrame(buffer)
    if (trailing) yield trailing
  } finally {
    await reader.cancel().catch(() => undefined)
  }
}

function parseFrame(frame: string): StreamEvent | null {
  const data = frame
    .split('\n')
    .filter((line) => line.startsWith('data:'))
    .map((line) => line.slice(5).trimStart())
    .join('\n')

  if (!data) return null
  try {
    return JSON.parse(data) as StreamEvent
  } catch {
    return null
  }
}
