import type {
  BatchEvent,
  Message,
  MessagePart,
  RetryInfo,
  Run,
  RunEvent,
  SnapshotEvent,
  ToolSlot,
} from '@openchat/api-client'

/**
 * A conversation's run in flight, as the events stream describes it.
 *
 * Everything here was committed on the server before it arrived, so this is a
 * view of server state that changes many times a second — which is why it
 * lives in a store and not in the query cache, and why it is handed back to
 * Query (by invalidating the message list) once the run is over.
 */
export interface LiveRun {
  run: Run
  /** Rows this run wrote, its input first. */
  messages: Message[]
  /** The response being streamed, as of the server's last commit. */
  partial: MessagePart[] | null
  retry: RetryInfo | null
  tools: Record<string, ToolSlot['status']>
  /** The run is compressing the conversation's context before its next request. */
  compacting: boolean
}

export interface ConversationLive {
  /**
   * The current run, or the one that just finished — kept until the message
   * list has been refetched with its rows, so the answer never blinks out in
   * between.
   */
  run: LiveRun | null
}

export const idle: ConversationLive = { run: null }

export function fromSnapshot(snapshot: SnapshotEvent): ConversationLive {
  if (!snapshot.run) return idle
  const generation = snapshot.live?.generation
  const tools: LiveRun['tools'] = {}
  for (const slot of snapshot.live?.tools ?? []) tools[slot.callId] = slot.status
  return {
    run: {
      run: snapshot.run,
      messages: snapshot.messages,
      partial: generation && generation.parts.length > 0 ? generation.parts : null,
      retry: generation?.retry ?? null,
      tools,
      compacting: snapshot.live?.compacting ?? false,
    },
  }
}

/** Applies the events of one commit. Pure: returns a new state. */
export function applyBatch(state: ConversationLive, batch: BatchEvent): ConversationLive {
  const current = state.run
  // Already contained in what we hold — the server filters these too, but a
  // locally started run can race its own first batch.
  if (current && current.run.id === batch.runId && batch.version <= current.run.version) {
    return state
  }

  let live: LiveRun | null = current && current.run.id === batch.runId ? { ...current } : null
  for (const event of batch.events) {
    live = applyEvent(live, event)
  }
  if (!live) return state
  return { run: { ...live, run: { ...live.run, version: Math.max(live.run.version, batch.version) } } }
}

function applyEvent(live: LiveRun | null, event: RunEvent): LiveRun | null {
  if (event.type === 'run_started') {
    // The event's copy of the question carries no attachments: their links are
    // signed per reader, which an event shared by every subscriber cannot be.
    // The copy the send response gave us has them, so it is kept.
    const held = live?.run.id === event.run.id ? live.messages.find((m) => m.id === event.message.id) : undefined
    const message =
      held && held.attachments?.length ? { ...event.message, attachments: held.attachments } : event.message
    return { run: event.run, messages: [message], partial: null, retry: null, tools: {}, compacting: false }
  }
  // Anything else belongs to a run we are holding; a stray event for one we
  // never saw start is ignored, and the next snapshot sets things straight.
  if (!live) return live

  switch (event.type) {
    case 'run_finished':
      return { ...live, run: event.run, partial: null, retry: null, compacting: false }
    case 'generation_started':
      return { ...live, partial: [], retry: null }
    case 'part_started':
      return { ...live, partial: setAt(live.partial ?? [], event.index, event.part) }
    case 'part_delta': {
      const parts = live.partial ?? []
      const part = parts[event.index]
      if (!part) return live
      return { ...live, partial: setAt(parts, event.index, appendDelta(part, event.delta)) }
    }
    case 'part_ended':
      return { ...live, partial: setAt(live.partial ?? [], event.index, event.part) }
    case 'message_appended': {
      if (live.messages.some((m) => m.id === event.message.id)) return live
      const messages = [...live.messages, event.message]
      // An assistant row is the finished form of the partial.
      const partial = event.message.role === 'assistant' ? null : live.partial
      return { ...live, messages, partial }
    }
    case 'tool_started':
      return { ...live, tools: { ...live.tools, [event.callId]: 'running' } }
    case 'tool_finished':
      return { ...live, tools: { ...live.tools, [event.callId]: 'done' } }
    case 'retry_scheduled':
      return { ...live, partial: null, retry: { at: event.at, errorCode: event.errorCode } }
    case 'compaction_started':
      return { ...live, compacting: true }
    case 'compaction_finished':
      return { ...live, compacting: false }
  }
  return live
}

function setAt(parts: MessagePart[], index: number, part: MessagePart): MessagePart[] {
  const next = parts.slice()
  while (next.length < index) next.push({ type: 'text', text: '' })
  next[index] = part
  return next
}

function appendDelta(part: MessagePart, delta: string): MessagePart {
  switch (part.type) {
    case 'text':
    case 'reasoning':
      return { ...part, text: part.text + delta }
    case 'tool_call':
      return { ...part, rawArguments: part.rawArguments + delta }
    default:
      return part
  }
}

export function isRunning(live: ConversationLive | undefined): boolean {
  return live?.run?.run.status === 'running'
}
