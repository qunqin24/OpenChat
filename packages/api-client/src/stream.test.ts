import { describe, expect, it } from 'vitest'

import { StreamIdleError, streamConversationEvents } from './stream'

/** A response whose body arrives as these chunks, as a server would send them. */
function streamed(chunks: (string | Uint8Array)[], { hang = false } = {}): typeof globalThis.fetch {
  return async (_input, init) => {
    const encoder = new TextEncoder()
    const body = new ReadableStream<Uint8Array>({
      start(controller) {
        for (const chunk of chunks) controller.enqueue(typeof chunk === 'string' ? encoder.encode(chunk) : chunk)
        if (!hang) controller.close()
        // As a real fetch does: aborting the request errors its body.
        init?.signal?.addEventListener('abort', () => controller.error(new DOMException('aborted', 'AbortError')))
      },
    })
    return new Response(body, { status: 200, headers: { 'Content-Type': 'text/event-stream' } })
  }
}

async function collect(fetch: typeof globalThis.fetch, idleTimeout = 1000) {
  const out: unknown[] = []
  for await (const event of streamConversationEvents({ baseUrl: 'http://x/api/v1', conversationId: 'c', fetch, idleTimeout })) {
    out.push(event)
  }
  return out
}

describe('streamConversationEvents', () => {
  it('reads frames split across chunks and skips keep-alives', async () => {
    const events = await collect(
      streamed([': keep-alive\n\n', 'data: {"type":"snap', 'shot"}\n', '\ndata: {"type":"batch","n":1}\n\n']),
    )
    expect(events).toEqual([{ type: 'snapshot' }, { type: 'batch', n: 1 }])
  })

  it('keeps a character whole when its bytes are split between chunks', async () => {
    const bytes = new TextEncoder().encode('data: {"text":"草莓"}\n\n')
    // "草" is three bytes; cut inside it.
    const cut = bytes.indexOf(0xe8) + 1
    const events = await collect(streamed([bytes.slice(0, cut), bytes.slice(cut)]))
    expect(events).toEqual([{ text: '草莓' }])
  })

  it('gives up on a connection that goes quiet', async () => {
    await expect(collect(streamed(['data: {"type":"snapshot"}\n\n'], { hang: true }), 50)).rejects.toBeInstanceOf(StreamIdleError)
  })

  it('uses the fetch it is given', async () => {
    let asked = ''
    const fetch: typeof globalThis.fetch = async (input, init) => {
      asked = `${String(input)} ${new Headers(init?.headers).get('Accept')}`
      return streamed([])(input, init)
    }
    await collect(fetch)
    expect(asked).toBe('http://x/api/v1/conversations/c/events text/event-stream')
  })
})
