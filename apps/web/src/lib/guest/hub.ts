import type { BatchEvent, ConversationEvent, SnapshotEvent } from '@openchat/api-client'

import { applyBatch, fromSnapshot, idle, type ConversationLive } from '@/lib/run-state'

/**
 * The events of a guest's conversations, made here instead of on the server.
 *
 * The page follows a conversation by reading its events stream (the snapshot of
 * the run in flight, then every batch), and for a guest that stream is this
 * object's: the runner (runner.ts) publishes what the server's run streams back
 * - once it is kept - and each page that follows a conversation gets it as
 * server-sent events from `eventStream`, with the same snapshot a reconnecting
 * page would be given by the server.
 */
export class GuestHub {
  private listeners = new Map<string, Set<(event: ConversationEvent) => void>>()
  private lives = new Map<string, ConversationLive>()

  subscribe(conversationId: string, listener: (event: ConversationEvent) => void): () => void {
    const set = this.listeners.get(conversationId) ?? new Set()
    set.add(listener)
    this.listeners.set(conversationId, set)
    return () => {
      set.delete(listener)
      if (set.size === 0) this.listeners.delete(conversationId)
    }
  }

  publish(conversationId: string, event: ConversationEvent) {
    // The same reducer the page applies, so a page that connects half way
    // through a run is told what a page that was there from the start holds.
    if (event.type === 'batch') this.lives.set(conversationId, applyBatch(this.live(conversationId), event))
    else this.lives.set(conversationId, fromSnapshot(event))
    for (const listener of this.listeners.get(conversationId) ?? []) listener(event)
  }

  live(conversationId: string): ConversationLive {
    return this.lives.get(conversationId) ?? idle
  }

  /** The run in flight, as a snapshot (what the server sends first on a stream). */
  snapshot(conversationId: string): SnapshotEvent {
    const run = this.live(conversationId).run
    if (!run || run.run.status !== 'running') return { type: 'snapshot', messages: [] }
    return {
      type: 'snapshot',
      run: run.run,
      messages: run.messages,
      live: {
        generation: run.partial || run.retry ? { attempt: 1, parts: run.partial ?? [], ...(run.retry ? { retry: run.retry } : {}) } : undefined,
        tools: Object.entries(run.tools).map(([callId, status]) => ({ callId, name: '', status })),
      },
    }
  }

  /** A batch of events that no run wrote: another page changed the conversation (deleted rows, or finished a run). */
  announceChanged(conversationId: string) {
    const batch: BatchEvent = {
      type: 'batch',
      runId: '00000000-0000-0000-0000-000000000000',
      version: 0,
      events: [{ type: 'transcript_changed' }],
    }
    this.publish(conversationId, batch)
  }

  /** A conversation's followers are told of something that is for everyone and not any run: the page that deleted it, say. */
  forget(conversationId: string) {
    this.lives.delete(conversationId)
  }
}

const encoder = new TextEncoder()

/**
 * A server-sent-events body for a conversation: its snapshot, then each event
 * published, with a keep-alive comment now and then so that the reader, which
 * takes silence for a dead connection, does not.
 */
export function eventStream(hub: GuestHub, conversationId: string, signal?: AbortSignal | null): Response {
  let stop = () => {}
  const body = new ReadableStream<Uint8Array>({
    start(controller) {
      const send = (text: string) => {
        try {
          controller.enqueue(encoder.encode(text))
        } catch {
          stop()
        }
      }
      send(`data: ${JSON.stringify(hub.snapshot(conversationId))}\n\n`)
      const unsubscribe = hub.subscribe(conversationId, (event) => send(`data: ${JSON.stringify(event)}\n\n`))
      const beat = setInterval(() => send(': ping\n\n'), 5000)
      stop = () => {
        unsubscribe()
        clearInterval(beat)
        try {
          controller.close()
        } catch {
          // Closed already.
        }
      }
      signal?.addEventListener('abort', stop, { once: true })
      if (signal?.aborted) stop()
    },
    cancel() {
      stop()
    },
  })
  return new Response(body, { headers: { 'Content-Type': 'text/event-stream', 'Cache-Control': 'no-cache' } })
}
