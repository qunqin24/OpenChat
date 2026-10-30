// A reply as the reader takes it: the process and the answer, as ChatGPT has
// it. Everything before the answer - the thinking, the words the model says
// on its way ("let me look that up"), its tool calls - is one fold, laid out
// as it happened (the web's ToolActivity, the app's ActivityLine); what
// follows it is the answer. Pure, so the web (MessageItem.tsx) and the app
// (Reply.tsx) fold a reply alike.

/**
 * What one entry of a reply is, in order: a thought, something the model
 * said, a tool call that is a step, or anything else drawn in the reply's own
 * flow (a document card, a note kept, a notice); `null` draws nothing.
 */
export type ReplyEntryKind = 'reasoning' | 'text' | 'tool' | 'other'

/** The process: entries `start` to `end`, both included. */
export interface ProcessSpan {
  start: number
  end: number
}

const inProcess = (kind: ReplyEntryKind | null) => kind === 'reasoning' || kind === 'text' || kind === 'tool'

/**
 * Where the process lies. It ends at the last tool call and the thinking
 * right after it; with no calls, at the thinking before the first words. It
 * starts at the first thought, word or call before that: the words before the
 * last call were said on the way, not the answer. Anything else inside the
 * span is drawn after the fold, in order. Null when the reply has no process
 * (it only answered).
 */
export function processSpan(kinds: readonly (ReplyEntryKind | null)[]): ProcessSpan | null {
  let end = kinds.lastIndexOf('tool')
  if (end < 0) {
    const firstText = kinds.indexOf('text')
    end = (firstText < 0 ? kinds : kinds.slice(0, firstText)).lastIndexOf('reasoning')
  }
  if (end < 0) return null
  for (let i = end + 1; i < kinds.length; i++) {
    if (kinds[i] === null) continue
    if (kinds[i] !== 'reasoning') break
    end = i
  }
  return { start: kinds.findIndex(inProcess), end }
}

/** What the model is doing in the process with no call in flight, if anything. */
export type ProcessThinking = 'reasoning' | 'waiting' | 'retrying' | false

/**
 * `reasoning` while its thoughts stream; `waiting` when the run is quiet
 * between a round and its next response (the line keeps naming the step just
 * done); `retrying` when a failed request is tried again. Only while the
 * process is the newest thing in the reply: once the answer has started, it
 * is done, whatever the run is still doing.
 */
export function processThinking(
  kinds: readonly (ReplyEntryKind | null)[],
  span: ProcessSpan,
  run: {
    running: boolean
    compacting: boolean
    retrying: boolean
    /** A tool call is in flight. */
    toolRunning: boolean
    /** The response being written has nothing in it yet. */
    partialEmpty: boolean
    /** The last entry of the span is a thought still being written. */
    thoughtStreaming: boolean
  },
): ProcessThinking {
  if (!run.running || run.compacting) return false
  if (kinds.slice(span.end + 1).some((kind) => kind !== null)) return false
  if (run.retrying) return 'retrying'
  if (run.thoughtStreaming) return 'reasoning'
  if (!run.toolRunning && run.partialEmpty) return 'waiting'
  return false
}
