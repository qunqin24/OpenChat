import {
  isOpenChatError,
  streamGuestRun,
  type BatchEvent,
  type GuestEndEvent,
  type GuestRunEvent,
  type Message,
  type ReasoningEffort,
  type Run,
} from '@openchat/api-client'

import { GuestError } from './errors'
import type { GuestHub } from './hub'
import type { GuestRepo } from './repo'
import { buildRunRequest, type RunAction } from './request'

/** What the composer asked this run for. */
export interface RunChoice {
  model?: string
  reasoningEffort?: ReasoningEffort
  webSearch?: boolean
}

export interface RunnerDeps {
  repo: GuestRepo
  hub: GuestHub
  /** The API's base URL: /guest/runs is posted under it. */
  baseUrl: string
  /** The network's fetch, never the page's own (which would serve this locally). */
  fetch: typeof globalThis.fetch
  /** Names this page, so that a run whose page went away can be told from one that is slow. */
  tab: string
  /** The run, or its title and memories, are kept: queries over them are stale. */
  onSettled?: (conversationId: string) => void
  /** Rows were deleted by a redo that has started: other pages showing the conversation hold them. */
  onDeleted?: (conversationId: string, from?: string) => void
}

/** How often a run says it is still being followed. */
const BEAT_MS = 5000

interface Active {
  runId: string
  controller: AbortController
  /** Resolves when the stream has been read to its end and everything it brought is kept. */
  done: Promise<void>
}

/**
 * Runs a guest's questions. A run is one request to the server (`/guest/runs`)
 * that stays open as long as the answer takes. What it streams back is kept
 * here as it arrives - the rows in the repository, the events in the hub - and
 * in that order, so that a page told a run finished finds what it wrote when
 * it asks for the transcript. The server keeps nothing: when the stream has
 * ended it is what this has stored that is the conversation.
 */
export class GuestRunner {
  private readonly active = new Map<string, Active>()

  constructor(private readonly deps: RunnerDeps) {}

  isRunning(conversationId: string): boolean {
    return this.active.has(conversationId)
  }

  /**
   * Starts a run and resolves once the server has taken it - the first batch
   * is in, so the run has an id and the question is kept. The rest is read in
   * the background. A request the server refuses throws its error, and nothing
   * has been written.
   */
  async start(
    conversationId: string,
    what: RunAction,
    choice: RunChoice,
  ): Promise<{ run: Run; inputId: string; written: Message[] }> {
    const { repo, hub } = this.deps
    // The previous run's last frame - the memories it changed, its title - is
    // what this one is asked with.
    await this.active.get(conversationId)?.done
    if (this.active.has(conversationId)) throw new GuestError(409, 'conversation_busy', 'this conversation is already answering')

    const data = await repo.data(conversationId)
    if (repo.activeRun(data)) throw new GuestError(409, 'conversation_busy', 'this conversation is already answering')
    const built = await buildRunRequest(
      {
        data,
        ...choice,
        personalization: await repo.storedPersonalization(),
        memories: await repo.memories(),
        readBlob: (id) => repo.file(id),
      },
      what,
    )

    const controller = new AbortController()
    const frames = streamGuestRun({
      baseUrl: this.deps.baseUrl,
      request: built.request,
      signal: controller.signal,
      fetch: this.deps.fetch,
    })[Symbol.asyncIterator]()

    let first: BatchEvent | undefined
    try {
      for (;;) {
        const next = await frames.next()
        if (next.done) break
        if (next.value.type === 'batch' && next.value.events.some((e) => e.type === 'run_started')) {
          first = next.value
          break
        }
      }
    } catch (error) {
      controller.abort()
      throw asGuestError(error)
    }
    const started = first?.events.find((e) => e.type === 'run_started')
    if (!first || started?.type !== 'run_started') {
      controller.abort()
      throw new GuestError(502, 'guest_unreachable', 'the server did not start the run')
    }

    await repo.runStarted(
      conversationId,
      {
        run: started.run,
        input: started.message,
        action: built.request.action,
        replaces: built.input?.replaces,
        attachments: built.input?.blobs,
        choice,
      },
      this.deps.tab,
    )
    if (what.action === 'edit' || what.action === 'regenerate') this.deps.onDeleted?.(conversationId, built.input?.replaces)
    const written: Message[] = []
    for (const event of first.events) {
      if (event.type === 'message_appended') {
        await repo.messageAppended(conversationId, event.message)
        written.push(event.message)
      }
    }
    hub.publish(conversationId, first)

    const done = this.pump(conversationId, started.run.id, frames, controller).finally(() => {
      if (this.active.get(conversationId)?.runId === started.run.id) this.active.delete(conversationId)
      this.deps.onSettled?.(conversationId)
    })
    this.active.set(conversationId, { runId: started.run.id, controller, done })
    return { run: started.run, inputId: started.message.id, written }
  }

  /** Resolves when the conversation's run, if any, has been read to its end and kept. */
  async settled(conversationId: string): Promise<void> {
    await this.active.get(conversationId)?.done
  }

  /** Stops a run. What it generated so far is kept: the stream goes on to its end. */
  async cancel(runId: string): Promise<void> {
    const base = this.deps.baseUrl.replace(/\/$/, '')
    await this.deps.fetch(`${base}/guest/runs/${encodeURIComponent(runId)}/cancel`, { method: 'POST' })
  }

  /** Lets go of a conversation's run, as when the conversation is deleted. */
  abort(conversationId: string) {
    this.active.get(conversationId)?.controller.abort()
  }

  private async pump(
    conversationId: string,
    runId: string,
    frames: AsyncIterator<GuestRunEvent>,
    controller: AbortController,
  ): Promise<void> {
    const { repo, hub } = this.deps
    const beat = setInterval(() => void repo.heartbeat(conversationId, runId).catch(() => undefined), BEAT_MS)
    let ended = false
    try {
      for (;;) {
        const next = await frames.next()
        if (next.done) break
        const frame = next.value
        if (frame.type === 'guest_end') {
          await this.end(conversationId, frame)
          ended = true
          continue
        }
        if (frame.type === 'snapshot') {
          for (const message of frame.messages) await repo.messageAppended(conversationId, message)
          if (frame.run && frame.run.status !== 'running') await repo.runUpdated(conversationId, frame.run)
          hub.publish(conversationId, frame)
          continue
        }
        for (const event of frame.events) {
          // Kept before it is told: a page that hears a run finished asks for its rows.
          if (event.type === 'message_appended') await repo.messageAppended(conversationId, event.message)
          if (event.type === 'run_finished') await repo.runUpdated(conversationId, event.run)
          // The context the run compressed is kept as soon as it is told: it
          // ends at a row this page already holds (the rows came first), and a
          // connection lost before the last frame must not lose it.
          if (event.type === 'compaction_finished' && event.summary) await repo.setSummary(conversationId, event.summary)
        }
        hub.publish(conversationId, frame)
      }
    } catch (error) {
      if (!controller.signal.aborted) console.warn('guest run stream failed', error)
    } finally {
      clearInterval(beat)
    }
    if (ended) return

    // The stream ended without its last frame: the connection was lost.
    const lost = await repo.runInterrupted(conversationId, runId, 'connection_lost').catch(() => null)
    if (!lost) return
    hub.publish(conversationId, {
      type: 'batch',
      runId,
      version: lost.run.version,
      events: [
        { type: 'message_appended', message: lost.message },
        { type: 'run_finished', run: lost.run },
      ],
    })
  }

  private async end(conversationId: string, frame: GuestEndEvent) {
    const { repo, hub } = this.deps
    await repo.runEnded(conversationId, frame)
    if (frame.title) {
      // Told as the server tells it: outside the run, under the run's last version.
      hub.publish(conversationId, {
        type: 'batch',
        runId: frame.run.id,
        version: frame.run.version,
        events: [{ type: 'title_changed', title: frame.title }],
      })
    }
  }
}

/** A refusal of the server, or a connection that did not happen, as the answer to the page's own request. */
function asGuestError(error: unknown): GuestError {
  if (isOpenChatError(error)) return new GuestError(error.status, error.code, error.message, error.field)
  return new GuestError(502, 'guest_unreachable', 'the server could not be reached')
}
