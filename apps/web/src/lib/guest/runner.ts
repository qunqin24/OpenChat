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
  progress: Progress
}

/** How far a run has got, as the pump learns it. */
interface Progress {
  /**
   * The run has been told finished. Only its closing frame (memories, title) is
   * still to come, and a question asked now waits for that rather than being refused.
   */
  finished: boolean
}

const busy = () => new GuestError(409, 'conversation_busy', 'this conversation is already answering')

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
  /** Conversations a run is being started for: the time before it is in `active`. */
  private readonly starting = new Set<string>()

  constructor(private readonly deps: RunnerDeps) {}

  isRunning(conversationId: string): boolean {
    return this.active.has(conversationId)
  }

  /**
   * Starts a run and resolves once the server has taken it - the first batch
   * is in, so the run has an id and the question is kept. The rest is read in
   * the background. A request the server refuses throws its error, and nothing
   * has been written.
   *
   * One run at a time per conversation, as on the server: a question asked
   * while the conversation is answering is refused (`conversation_busy`), not
   * queued behind the answer to be sent when it ends, unasked.
   */
  async start(
    conversationId: string,
    what: RunAction,
    choice: RunChoice,
  ): Promise<{ run: Run; inputId: string; written: Message[] }> {
    const current = this.active.get(conversationId)
    if ((current && !current.progress.finished) || this.starting.has(conversationId)) throw busy()
    this.starting.add(conversationId)
    try {
      // A run that has been told finished is only reading its last frame - the
      // memories it changed, its title - which is what this one is asked with.
      await current?.done
      return await this.begin(conversationId, what, choice)
    } finally {
      this.starting.delete(conversationId)
    }
  }

  private async begin(
    conversationId: string,
    what: RunAction,
    choice: RunChoice,
  ): Promise<{ run: Run; inputId: string; written: Message[] }> {
    const { repo, hub } = this.deps
    if (this.active.has(conversationId)) throw busy()

    const data = await repo.data(conversationId)
    if (repo.activeRun(data)) throw busy()
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

    const written: Message[] = []
    try {
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
      for (const event of first.events) {
        if (event.type === 'message_appended') {
          await repo.messageAppended(conversationId, event.message)
          written.push(event.message)
        }
      }
    } catch (error) {
      // The server has accepted the run but the browser could not keep it (the
      // storage failed). Nobody is going to read this stream, so the server's
      // run must not be left generating - and being paid for - with no one to
      // show it to. Closing the request cancels it.
      controller.abort()
      void repo.runInterrupted(conversationId, started.run.id, 'storage_failed').catch(() => undefined)
      throw error
    }
    hub.publish(conversationId, first)

    const progress: Progress = { finished: false }
    const done = this.pump(conversationId, started.run, frames, controller, progress).finally(() => {
      if (this.active.get(conversationId)?.runId === started.run.id) this.active.delete(conversationId)
      this.deps.onSettled?.(conversationId)
    })
    this.active.set(conversationId, { runId: started.run.id, controller, done, progress })
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
    started: Run,
    frames: AsyncIterator<GuestRunEvent>,
    controller: AbortController,
    progress: Progress,
  ): Promise<void> {
    const { repo, hub } = this.deps
    const runId = started.id
    const beat = setInterval(() => void repo.heartbeat(conversationId, runId).catch(() => undefined), BEAT_MS)
    // The newest version told to the page, so that a failure told after it is newer.
    let version = started.version
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
          if (frame.run && frame.run.status !== 'running') {
            await repo.runUpdated(conversationId, frame.run)
            progress.finished = true
          }
          if (frame.run) version = Math.max(version, frame.run.version)
          hub.publish(conversationId, frame)
          continue
        }
        for (const event of frame.events) {
          // Kept before it is told: a page that hears a run finished asks for its rows.
          if (event.type === 'message_appended') await repo.messageAppended(conversationId, event.message)
          if (event.type === 'run_finished') {
            await repo.runUpdated(conversationId, event.run)
            progress.finished = true
          }
          // The context the run compressed is kept as soon as it is told: it
          // ends at a row this page already holds (the rows came first), and a
          // connection lost before the last frame must not lose it.
          if (event.type === 'compaction_finished' && event.summary) await repo.setSummary(conversationId, event.summary)
        }
        version = Math.max(version, frame.version)
        hub.publish(conversationId, frame)
      }
    } catch (error) {
      if (!controller.signal.aborted) console.warn('guest run stream failed', error)
      // Whatever went wrong - the connection, or keeping what came - nothing more
      // will be read: the server's run must not be left going.
      controller.abort()
    } finally {
      clearInterval(beat)
    }
    if (ended) return

    // The stream ended without its last frame: the connection was lost, or the
    // storage failed.
    let lost: Awaited<ReturnType<GuestRepo['runInterrupted']>>
    try {
      lost = await repo.runInterrupted(conversationId, runId, 'connection_lost')
    } catch {
      // The failure could not be written down either (a storage that fails is
      // likely to fail again). The page still has to be told the run is over, or
      // its composer shows Stop for a run nothing is reading. A run that
      // was already told finished needs no telling again.
      if (!progress.finished) {
        progress.finished = true
        hub.publish(conversationId, {
          type: 'batch',
          runId,
          version: version + 1,
          events: [
            {
              type: 'run_finished',
              run: { ...started, status: 'failed', errorCode: 'connection_lost', finishedAt: new Date().toISOString(), version: version + 1 },
            },
          ],
        })
      }
      return
    }
    // Null: the run is over in the repository already (it was told), or its
    // conversation is gone.
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
  if (isOpenChatError(error)) return new GuestError(error.status, error.code, error.message, error.field, error.details)
  return new GuestError(502, 'guest_unreachable', 'the server could not be reached')
}
