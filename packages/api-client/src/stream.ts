import { OpenChatError } from './errors'
import type { ConversationEvent, GuestRunEvent, GuestRunRequest } from './types'

export interface ConversationEventsOptions {
  baseUrl: string
  conversationId: string
  signal?: AbortSignal
  headers?: HeadersInit
  /** Pass 'include' so the browser carries the HttpOnly session cookie. */
  credentials?: RequestCredentials
  /**
   * Milliseconds without a single byte — event or keep-alive — after which
   * the connection is taken for dead and the stream ends with a
   * `StreamIdleError`. The server sends a keep-alive every 5 seconds.
   */
  idleTimeout?: number
  /**
   * What makes the request. The page's own, when the events do not come from
   * the network - a guest's conversations are kept in the browser, and their
   * events are made there.
   */
  fetch?: typeof globalThis.fetch
}

/**
 * The connection went quiet for longer than the server's keep-alives allow.
 *
 * A connection can die without closing: the laptop slept, the network
 * changed, a proxy dropped it. Reading it then waits forever, and the page
 * sits on half an answer while the run finishes without it. Thrown so the
 * caller reconnects, which starts from a fresh snapshot.
 */
export class StreamIdleError extends Error {
  constructor(ms: number) {
    super(`no data on the event stream for ${ms}ms`)
    this.name = 'StreamIdleError'
  }
}

/**
 * Follows a conversation's runs: yields the server's `snapshot`, then every
 * `batch` committed after it, until the signal aborts or the connection drops.
 *
 * openapi-fetch cannot model a streaming response body, so this is hand-written
 * against the same contract. Frames are `data: <json>` separated by a blank
 * line; `:` comment frames are keep-alives and are skipped. Reconnecting is the
 * caller's job — a new connection starts from a fresh snapshot, so there is no
 * position to resume from.
 */
export async function* streamConversationEvents(
  options: ConversationEventsOptions,
): AsyncGenerator<ConversationEvent, void, undefined> {
  const { baseUrl, conversationId, signal, headers, credentials, idleTimeout = 15_000 } = options
  yield* readFrames<ConversationEvent>(
    `${baseUrl.replace(/\/$/, '')}/conversations/${conversationId}/events`,
    { headers: { Accept: 'text/event-stream', ...headers }, credentials },
    { signal, idleTimeout, fetch: options.fetch },
  )
}

export interface GuestRunOptions {
  baseUrl: string
  request: GuestRunRequest
  signal?: AbortSignal
  idleTimeout?: number
  fetch?: typeof globalThis.fetch
}

/**
 * A guest's run: posts the conversation as the guest's browser holds it and
 * yields the events of the run, then the closing frame (`guest_end`), as the
 * server commits them. A request the server refuses before it starts (a limit,
 * a model that is not offered) throws an `OpenChatError` with its body; the
 * generator does nothing until it is first read.
 */
export async function* streamGuestRun(options: GuestRunOptions): AsyncGenerator<GuestRunEvent, void, undefined> {
  const { baseUrl, request, signal, idleTimeout = 15_000 } = options
  yield* readFrames<GuestRunEvent>(
    `${baseUrl.replace(/\/$/, '')}/guest/runs`,
    {
      method: 'POST',
      headers: { Accept: 'text/event-stream', 'Content-Type': 'application/json' },
      body: JSON.stringify(request),
    },
    { signal, idleTimeout, fetch: options.fetch },
  )
}

/** Sends the request and yields the `data:` frames of its event-stream body. */
async function* readFrames<T>(
  url: string,
  init: RequestInit,
  { signal, idleTimeout, fetch: send = globalThis.fetch }: { signal?: AbortSignal; idleTimeout: number; fetch?: typeof globalThis.fetch },
): AsyncGenerator<T, void, undefined> {
  // Our own controller, so the watchdog can end a connection the caller did
  // not; the caller's signal still ends it too.
  const controller = new AbortController()
  const abort = () => controller.abort(signal?.reason)
  if (signal?.aborted) abort()
  else signal?.addEventListener('abort', abort, { once: true })

  let idle = false
  let timer: ReturnType<typeof setTimeout> | undefined
  const watch = () => {
    clearTimeout(timer)
    timer = setTimeout(() => {
      idle = true
      controller.abort()
    }, idleTimeout)
  }

  watch()
  let response: Response
  try {
    response = await send(url, { ...init, signal: controller.signal })
  } catch (error) {
    clearTimeout(timer)
    signal?.removeEventListener('abort', abort)
    throw idle ? new StreamIdleError(idleTimeout) : error
  }

  if (!response.ok || !response.body) {
    clearTimeout(timer)
    signal?.removeEventListener('abort', abort)
    if (!response.ok) throw await OpenChatError.fromResponse(response)
    throw new OpenChatError(response.status, {
      code: 'no_stream_body',
      message: 'the response had no readable body',
    })
  }

  const reader = response.body.pipeThrough(new TextDecoderStream()).getReader()
  let buffer = ''

  try {
    while (true) {
      let chunk: ReadableStreamReadResult<string>
      try {
        chunk = await reader.read()
      } catch (error) {
        throw idle ? new StreamIdleError(idleTimeout) : error
      }
      const { done, value } = chunk
      if (done) break
      watch()

      buffer += value
      let boundary = buffer.indexOf('\n\n')
      while (boundary !== -1) {
        const frame = buffer.slice(0, boundary)
        buffer = buffer.slice(boundary + 2)
        const event = parseFrame<T>(frame)
        if (event) yield event
        boundary = buffer.indexOf('\n\n')
      }
    }
  } finally {
    clearTimeout(timer)
    signal?.removeEventListener('abort', abort)
    await reader.cancel().catch(() => undefined)
  }
}

function parseFrame<T>(frame: string): T | null {
  const data = frame
    .split('\n')
    .filter((line) => line.startsWith('data:'))
    .map((line) => line.slice(5).trimStart())
    .join('\n')

  if (!data) return null
  try {
    return JSON.parse(data) as T
  } catch {
    return null
  }
}
