import type { Message, MessagePart, ToolResultPart } from '@openchat/api-client'

import type { LiveRun } from './run-state'

/**
 * What the chat screen draws: the user's turns, and between them the
 * assistant's — one block per run, however many rows the run wrote.
 *
 * A run that searched twice and then answered is five rows on the server
 * (response with calls, two results, response with calls…); the reader sees one
 * reply with its tool steps inside it, in order.
 */
export type Turn =
  | { kind: 'user'; key: string; message: Message }
  | {
      kind: 'assistant'
      key: string
      /** Response rows in order; tool rows are folded into `results`. */
      responses: Message[]
      results: Record<string, ToolResultPart>
      /** The response still streaming, if this is the run in flight. */
      partial: MessagePart[] | null
      live: LiveRun | null
    }

export type AssistantTurn = Extract<Turn, { kind: 'assistant' }>

/**
 * Merges the persisted rows with the run in flight and groups them into turns.
 * Rows the live run holds that the message list already has are not repeated.
 */
export function buildTurns(persisted: Message[], live: LiveRun | null): Turn[] {
  const seen = new Set(persisted.map((m) => m.id))
  const rows = [...persisted, ...(live?.messages ?? []).filter((m) => !seen.has(m.id))]

  const turns: Turn[] = []
  let current: AssistantTurn | null = null

  const assistantTurn = (key: string): AssistantTurn => {
    const turn: AssistantTurn = { kind: 'assistant', key, responses: [], results: {}, partial: null, live: null }
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

    // A new run, or rows from before runs existed, start their own block.
    let turn: AssistantTurn | null = current
    if (!turn || (message.runId && turn.key !== message.runId)) {
      turn = assistantTurn(message.runId ?? message.id)
      current = turn
    }
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
    if (!last || last.kind !== 'assistant' || last.key !== live.run.id) {
      if (live.run.status !== 'running') return turns
      last = assistantTurn(live.run.id)
    }
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
