import type { ContextSummary, Message, MessagePart, ToolResultPart } from '@openchat/api-client'

import { ASK_USER } from './ask-user'
import type { LiveRun } from './run-state'
import { isReportRow, parseReports, type ReportNotice } from './subagents'

/**
 * What the chat screen draws: the user's turns, and between them the
 * assistant's — one block per run, however many rows the run wrote.
 *
 * A run that searched twice and then answered is five rows on the server
 * (response with calls, two results, response with calls…); the reader sees one
 * reply with its tool steps inside it, in order. So is a reply that stopped to
 * ask a question: the run that asked and the run the answer started share one
 * block, keyed by the first of them.
 *
 * A message content moderation refused (a `blocked` row) is a user turn like any
 * other, followed by a `refusal` turn: the reply that stands where the model's
 * answer would have been. Neither is part of what the conversation asked or
 * was answered (`sentTurns`).
 *
 * A row that delivers sub-agents' reports (`origin: 'subagent'`) is no
 * question of the person's. Delivered while the conversation was idle, it is
 * the input of the run started for it: a `reports` turn, one line per report,
 * before that run's reply. Delivered into a run that was going, it is part of
 * that run's reply, drawn among its parts (`AssistantTurn.reports`).
 */
export type Turn =
  | { kind: 'user'; key: string; message: Message; after?: ContextSummary }
  | {
      kind: 'reports'
      key: string
      message: Message
      notices: ReportNotice[]
      after?: ContextSummary
    }
  | {
      kind: 'refusal'
      key: string
      /** The refused message, which the reply is to. */
      message: Message
      /** Never a place for a divider: a summary does not end at a refused message. */
      after?: undefined
      /**
       * The refused message is the last row there is: the conversation stops here
       * until it is reworded. (Not when a row was written after it - old data -
       * even though that row may be drawn above it, in the reply it belongs to.)
       */
      last?: boolean
    }
  | {
      kind: 'assistant'
      key: string
      /**
       * The conversation's summary ends with this reply: the page draws its
       * divider just after it. (Rows the summary covers are never hidden; it
       * only marks where it stops.)
       */
      after?: ContextSummary
      /**
       * The summary ends inside this reply — a compression made between two
       * tool rounds of one run: the divider is drawn after the response with
       * this index (counting from 1), among the reply's own parts.
       */
      inside?: { index: number; summary: ContextSummary }
      /** Response rows in order; tool rows are folded into `results`. */
      responses: Message[]
      /**
       * Sub-agents' reports delivered into this reply while it was being
       * written, each after the response with index `index` (counting from 1;
       * 0 is before the first). Kept out of the user's turns; not drawn (the
       * launch line says how each sub-agent stands).
       */
      reports: { index: number; message: Message; notices: ReportNotice[] }[]
      /** Every run that wrote a row of this reply: more than one when it went on after a question. */
      runs: string[]
      results: Record<string, ToolResultPart>
      /** The response still streaming, if this is the run in flight. */
      partial: MessagePart[] | null
      live: LiveRun | null
    }

export type AssistantTurn = Extract<Turn, { kind: 'assistant' }>

/** A refused message or the reply that says so. */
export function isRefusedTurn(turn: Turn): boolean {
  return turn.kind === 'refusal' || (turn.kind === 'user' && turn.message.status === 'blocked')
}

/**
 * The turns without the refused messages and their replies: the conversation as
 * it was asked and answered. The last question, the answer a regeneration
 * replaces and the question the model is waiting on are all found among these,
 * as the server finds them among the rows it does not call blocked.
 */
export function sentTurns(turns: Turn[]): Exclude<Turn, { kind: 'refusal' }>[] {
  return turns.filter((turn): turn is Exclude<Turn, { kind: 'refusal' }> => !isRefusedTurn(turn))
}

/**
 * The refused message the conversation stops on: the last turn is a refusal, so
 * the last row is a message content moderation refused. Until it is reworded the
 * conversation does not go on (the server answers 409 `revise_refused` to a
 * send, a regeneration and an answer), so the page turns the composer into the
 * editor of this message (`composerMode`) and offers nothing else. A refused
 * message with something after it (old data) is not one the conversation stops
 * on.
 */
export function refusedTail(turns: Turn[]): Message | null {
  const last = turns.at(-1)
  return last?.kind === 'refusal' && last.last ? last.message : null
}

/**
 * The answer that can be asked for again, and the question it answers: the last
 * reply, only the last, as asking for an earlier one again would delete the turns
 * after it. A message moderation refused after it is no question and its reply no
 * answer (the server finds the last question the same way), and goes with the
 * answer when it is redone - but not while that message ends the conversation: it
 * is reworded first, and nothing is offered to redo (the server refuses it with
 * `revise_refused`).
 */
export function regenerable(turns: Turn[]): { answer: AssistantTurn; inputId: string } | null {
  if (refusedTail(turns)) return null
  const sent = sentTurns(turns)
  const answer = sent.at(-1)
  const input = [...sent].reverse().find((turn) => turn.kind === 'user')
  if (answer?.kind !== 'assistant' || input?.kind !== 'user') return null
  return { answer, inputId: input.message.id }
}

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
export function buildTurns(
  persisted: Message[],
  live: LiveRun | null,
  replacing?: Replacing,
  summary?: ContextSummary,
): Turn[] {
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
  // The refusal card of the refused message that is the last row so far.
  let tail: Extract<Turn, { kind: 'refusal' }> | null = null
  // How many responses the reply had when the summary's last row was written,
  // for the reply that holds it.
  const endsAt = new Map<AssistantTurn, number>()

  const assistantTurn = (key: string): AssistantTurn => {
    const turn: AssistantTurn = { kind: 'assistant', key, responses: [], reports: [], runs: [], results: {}, partial: null, live: null }
    turns.push(turn)
    return turn
  }

  for (const message of rows) {
    if (message.role === 'user' || message.role === 'assistant' || message.role === 'tool') tail = null
    if (isReportRow(message)) {
      const notices = parseReports(message.content)
      if (message.runId) {
        // Delivered into the run that was going: a line of its reply.
        const turn: AssistantTurn = current ?? assistantTurn(message.runId)
        current = turn
        if (!turn.runs.includes(message.runId)) turn.runs.push(message.runId)
        turn.reports.push({ index: turn.responses.length, message, notices })
        continue
      }
      // The input of the run started for it, which is a reply of its own.
      current = null
      turns.push({ kind: 'reports', key: message.id, message, notices, ...(message.id === summary?.throughMessageId ? { after: summary } : {}) })
      continue
    }
    if (message.role === 'user') {
      const refused = message.status === 'blocked'
      // A refused message is no question: a reply that went on after the
      // model's own question (the answer is written after it) still belongs to
      // the block that asked.
      if (!refused) current = null
      turns.push({ kind: 'user', key: message.id, message, ...(message.id === summary?.throughMessageId ? { after: summary } : {}) })
      if (refused) {
        tail = { kind: 'refusal', key: `${message.id}:refused`, message }
        turns.push(tail)
      }
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
    if (summary && message.id === summary.throughMessageId) endsAt.set(turn, turn.responses.length)
  }

  // Where the summary stops in a reply: after it when nothing else was said in
  // it since, and among its parts when the run went on (a compression between
  // two tool rounds) or is going on - the next thing it says is below the line.
  if (tail) tail.last = true

  const placeSummary = (): Turn[] => {
    for (const [turn, count] of endsAt) {
      if (!summary) break
      const going = turn.live?.run.status === 'running'
      if (count >= turn.responses.length && !going) turn.after = summary
      else turn.inside = { index: count, summary }
    }
    return turns
  }

  if (live) {
    // The run in flight owns the last block, which may not exist yet: before
    // its first response there is only the user's input. (A message refused
    // before it is no block.)
    let last = sentTurns(turns).at(-1)
    // One that continues a reply writes into the block that asked.
    if (!last || last.kind !== 'assistant' || (last.key !== live.run.id && !live.run.continues)) {
      if (live.run.status !== 'running') return placeSummary()
      last = assistantTurn(live.run.id)
    }
    if (!last.runs.includes(live.run.id)) last.runs.push(live.run.id)
    last.partial = live.partial
    last.live = live
  }
  return placeSummary()
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
  // A message refused after the question neither answers it nor hides it - but
  // while it ends the conversation the question cannot be answered past it: the
  // refused message is reworded first.
  const last = sentTurns(turns).at(-1)
  if (busy || last?.kind !== 'assistant' || refusedTail(turns)) return null
  const response = last.responses.at(-1)
  if (response?.stopReason !== 'tool_calls') return null
  for (const part of response.parts) {
    if (part.type === 'tool_call' && part.name === ASK_USER && !last.results[part.id]) return part.id
  }
  return null
}
