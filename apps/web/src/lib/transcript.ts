import type { Message, MessagePart, ToolResultPart } from '@openchat/api-client'

import { ASK_USER } from './ask-user'
import type { LiveRun } from './run-state'

/**
 * What the chat screen draws: the user's turns, and between them the
 * assistant's — one block per run, however many rows the run wrote.
 *
 * A run that searched twice and then answered is five rows on the server
 * (response with calls, two results, response with calls…); the reader sees one
 * reply with its tool steps inside it, in order. So is a reply that stopped to
 * ask a question: the run that asked and the run the answer started share one
 * block, keyed by the first of them.
 */
export type Turn =
  | { kind: 'user'; key: string; message: Message }
  | {
      kind: 'assistant'
      key: string
      /** Response rows in order; tool rows are folded into `results`. */
      responses: Message[]
      /** Every run that wrote a row of this reply: more than one when it went on after a question. */
      runs: string[]
      results: Record<string, ToolResultPart>
      /** The response still streaming, if this is the run in flight. */
      partial: MessagePart[] | null
      live: LiveRun | null
    }

export type AssistantTurn = Extract<Turn, { kind: 'assistant' }>

/**
 * What a redo hides until the server has started it and the cache has caught up
 * with what it deleted: with `from`, that message and every row after it (an
 * edited question and what followed it); with `after`, every row after that
 * message (the last question's answer, being regenerated).
 */
export interface Replacing {
  from?: string
  after?: string
}

/**
 * Merges the persisted rows with the run in flight and groups them into turns.
 * Rows the live run holds that the message list already has are not repeated.
 *
 * A run in flight can be answering a question the list still holds an answer
 * to: a regeneration deleted that answer on the server, and the list has not
 * been fetched again yet. The run's input is in the list then, and whatever
 * follows it, other than the run's own rows, is the answer that is gone. So
 * does `replacing`, what a redo was just asked for and the server has not
 * started yet.
 */
export function buildTurns(persisted: Message[], live: LiveRun | null, replacing?: Replacing): Turn[] {
  // Where the run in flight redoes an answer: after its input, which the list
  // already holds.
  let redone = Infinity
  // A run that goes on after a question is no redo: the reply before it stays.
  const input = live?.messages[0]
  if (live && input?.role === 'user' && !live.run.continues) {
    const same = persisted.findIndex((m) => m.id === input.id)
    if (same >= 0) redone = same + 1
  }
  // Where a redo just asked for starts.
  const target = replacing?.from ?? replacing?.after
  const at = target ? persisted.findIndex((m) => m.id === target) : -1
  const asked = at < 0 ? Infinity : replacing?.from ? at : at + 1
  const kept = persisted.filter((m, i) => i < asked && (i < redone || m.runId === live?.run.id))
  const seen = new Set(kept.map((m) => m.id))
  const rows = [...kept, ...(live?.messages ?? []).filter((m) => !seen.has(m.id))]

  const turns: Turn[] = []
  let current: AssistantTurn | null = null

  const assistantTurn = (key: string): AssistantTurn => {
    const turn: AssistantTurn = { kind: 'assistant', key, responses: [], runs: [], results: {}, partial: null, live: null }
    turns.push(turn)
    return turn
  }

  for (const message of rows) {
    if (message.role === 'user') {
      current = null
      turns.push({ kind: 'user', key: message.id, message })
      continue
    }
    if (message.role !== 'assistant' && message.role !== 'tool') continue

    // The rows after a question belong to the block that asked it, and after
    // anything else a run starts its own: no two replies follow each other
    // without a question between them (a redone one is hidden).
    let turn: AssistantTurn | null = current
    if (!turn) {
      turn = assistantTurn(message.runId ?? message.id)
      current = turn
    }
    if (message.runId && !turn.runs.includes(message.runId)) turn.runs.push(message.runId)
    if (message.role === 'assistant') {
      turn.responses.push(message)
    } else {
      for (const part of message.parts) {
        if (part.type === 'tool_result') turn.results[part.callId] = part
      }
    }
  }

  if (live) {
    // The run in flight owns the last block, which may not exist yet: before
    // its first response there is only the user's input.
    let last = turns.at(-1)
    // One that continues a reply writes into the block that asked.
    if (!last || last.kind !== 'assistant' || (last.key !== live.run.id && !live.run.continues)) {
      if (live.run.status !== 'running') return turns
      last = assistantTurn(live.run.id)
    }
    if (!last.runs.includes(live.run.id)) last.runs.push(live.run.id)
    last.partial = live.partial
    last.live = live
  }
  return turns
}

/** The answer's plain text, for the copy button: the text of every response. */
export function turnText(turn: AssistantTurn): string {
  return turn.responses
    .flatMap((m) => m.parts)
    .filter((p): p is Extract<MessagePart, { type: 'text' }> => p.type === 'text')
    .map((p) => p.text)
    .join('')
    .trim()
}

/**
 * The question the conversation is waiting on, by call id: the last reply
 * ended on an ask_user call nobody has answered. `busy` is anything that
 * supersedes it — a run going, a message on its way — since writing something
 * else is how a question is skipped.
 */
export function pendingQuestion(turns: Turn[], busy: boolean): string | null {
  const last = turns.at(-1)
  if (busy || last?.kind !== 'assistant') return null
  const response = last.responses.at(-1)
  if (response?.stopReason !== 'tool_calls') return null
  for (const part of response.parts) {
    if (part.type === 'tool_call' && part.name === ASK_USER && !last.results[part.id]) return part.id
  }
  return null
}
