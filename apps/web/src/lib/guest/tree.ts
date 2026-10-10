import type { MessagePage } from '@openchat/api-client'

import type { ConversationData, StoredMessage, StoredRun } from './types'

/**
 * The conversation tree, as the server keeps it (AGENTS.md, "对话是一棵树"):
 * every user message - an input - names the input it follows, the reader is on
 * the path from the root to the conversation's current leaf, and the
 * transcript is that path with, for each input on it, the rows of its one
 * answer that is not superseded. These are the functions of the SQL in
 * 20261015000001_branches.sql and queries/messages.sql, over arrays instead of
 * tables, and they are all pure: the storage module reads and writes, these
 * decide.
 */

/**
 * A sort key for a time as the server writes it (RFC 3339 with up to
 * nanoseconds): UTC seconds, then the fraction at full width. Comparing two
 * keys as strings orders the instants - a Date would lose the microseconds that
 * tell two rows of one commit apart.
 */
export function timeKey(iso: string): string {
  const ms = Date.parse(iso)
  if (Number.isNaN(ms)) return '0000-00-00T00:00:00.000000000'
  const fraction = /\.(\d+)/.exec(iso)?.[1] ?? ''
  return `${new Date(ms).toISOString().slice(0, 19)}.${fraction.padEnd(9, '0').slice(0, 9)}`
}

/** `(created_at, id)` as one comparable string. */
export function rowKey(row: { createdAt: string; id: string }): string {
  return `${timeKey(row.createdAt)}|${row.id}`
}

export const byRow = (a: { createdAt: string; id: string }, b: { createdAt: string; id: string }) => {
  const x = rowKey(a)
  const y = rowKey(b)
  return x < y ? -1 : x > y ? 1 : 0
}

const isInput = (m: StoredMessage) => m.role === 'user'

/** The inputs of the branch shown, from the root to the current leaf (conversation_path, reversed). */
export function pathOf(data: ConversationData): StoredMessage[] {
  const byId = new Map(data.messages.map((m) => [m.id, m]))
  const path: StoredMessage[] = []
  const seen = new Set<string>()
  let at = data.conversation.currentInputId ? byId.get(data.conversation.currentInputId) : undefined
  // A chain that looped would end here, as the UNION in the SQL ends it.
  while (at && !seen.has(at.id)) {
    seen.add(at.id)
    path.push(at)
    at = at.parentInputId ? byId.get(at.parentInputId) : undefined
  }
  return path.reverse()
}

/**
 * Every row the reader sees and the model is sent (conversation_transcript):
 * the inputs on the path and the rows of each one's answer that is not
 * superseded, oldest first.
 */
export function transcriptOf(data: ConversationData): StoredMessage[] {
  const onPath = new Set(pathOf(data).map((m) => m.id))
  const hidden = new Set(data.runs.filter((r) => r.superseded).map((r) => r.id))
  const answers = new Set(data.runs.filter((r) => onPath.has(r.inputId) && !r.superseded).map((r) => r.id))
  return data.messages
    .filter((m) => (isInput(m) ? onPath.has(m.id) : m.runId ? answers.has(m.runId) && !hidden.has(m.runId) : false))
    .sort(byRow)
}

/** The runs that answered an input, oldest first. An answer that went on after a question is one version, named by the run that began it. */
export function answersOf(runs: StoredRun[], inputId: string): StoredRun[] {
  return runs.filter((r) => r.inputId === inputId && !r.continues).sort(byRow)
}

/** The versions of a question: it and its siblings (same parent), oldest first. Roots are each other's siblings. */
export function siblingsOf(messages: StoredMessage[], input: StoredMessage): StoredMessage[] {
  const parent = input.parentInputId ?? null
  return messages.filter((m) => isInput(m) && (m.parentInputId ?? null) === parent).sort(byRow)
}

/** Whether the branch shown passes through a version of this question - what can be switched to (PathHasVersionOf). */
export function pathHasVersionOf(data: ConversationData, parentInputId: string | null): boolean {
  return pathOf(data).some((m) => (m.parentInputId ?? null) === parentInputId)
}

/** Whether an input is on the branch shown (InputOnPath). */
export function onPath(data: ConversationData, inputId: string): boolean {
  return pathOf(data).some((m) => m.id === inputId)
}

/**
 * Where the reader lands when switching to a question: the end of the branch
 * below it that was last the one shown (PickBranchLeaf) - the leaf with the
 * newest `shownAt`, the question itself being a leaf when nothing follows it.
 */
export function pickBranchLeaf(messages: StoredMessage[], inputId: string): string {
  const children = new Map<string, StoredMessage[]>()
  for (const m of messages) {
    if (isInput(m) && m.parentInputId) children.set(m.parentInputId, [...(children.get(m.parentInputId) ?? []), m])
  }
  const byId = new Map(messages.map((m) => [m.id, m]))
  const leaves: StoredMessage[] = []
  const seen = new Set<string>()
  const walk = (id: string) => {
    if (seen.has(id)) return
    seen.add(id)
    const kids = children.get(id) ?? []
    const self = byId.get(id)
    if (kids.length === 0 && self) leaves.push(self)
    kids.forEach((k) => walk(k.id))
  }
  walk(inputId)
  // shown_at DESC NULLS LAST, created_at DESC, id DESC
  leaves.sort((a, b) => {
    const x = a.shownAt ?? null
    const y = b.shownAt ?? null
    if (x !== y) {
      if (x === null) return 1
      if (y === null) return -1
      return y - x
    }
    return -byRow(a, b)
  })
  return leaves[0]?.id ?? inputId
}

/** A strictly increasing clock for `shownAt`: two switches in one millisecond still have an order. */
let lastShown = 0
export function shownNow(now = Date.now()): number {
  lastShown = Math.max(now, lastShown + 1)
  return lastShown
}

/**
 * Moves the reader to an input, a leaf of the conversation, noting that it was
 * the branch shown just now (SetCurrentInput). Returns the changed rows.
 */
export function setCurrentInput(data: ConversationData, inputId: string, now?: number): ConversationData {
  return {
    ...data,
    conversation: { ...data.conversation, currentInputId: inputId },
    messages: data.messages.map((m) => (m.id === inputId ? { ...m, shownAt: shownNow(now) } : m)),
  }
}

/**
 * Shows one of an input's answers and hides the rest; with none to keep, hides
 * them all, ahead of a new one (ChooseAnswer). A run that continues another
 * goes or stays with it.
 */
export function chooseAnswer(runs: StoredRun[], inputId: string, keep: string | null): StoredRun[] {
  return runs.map((r) => (r.inputId === inputId ? { ...r, superseded: (r.continues ?? r.id) !== keep } : r))
}

/** The rows of one answer, input first: what the server's ListRunMessages reads. */
export function runRows(data: ConversationData, runId: string, inputId: string): StoredMessage[] {
  const joined = new Set([runId, ...data.runs.filter((r) => r.continues === runId).map((r) => r.id)])
  return data.messages.filter((m) => m.id === inputId || (m.runId && joined.has(m.runId))).sort(byRow)
}

// ----------------------------------------------------------------- paging

/** A cursor is opaque to the page; this one is the key of the row it stands before. */
export const encodeCursor = (row: { createdAt: string; id: string }) => rowKey(row)

/**
 * A page of the transcript from its end (ListMessages): the newest rows, or
 * with a cursor the rows older than it, read oldest first; `olderCursor` is the
 * page's first row's when anything older remains.
 */
export function pageOf<T extends { createdAt: string; id: string }>(
  rows: T[],
  before: string | undefined,
  limit: number,
): { items: T[]; olderCursor: string | null } {
  const ordered = [...rows].sort(byRow)
  const older = before ? ordered.filter((r) => rowKey(r) < before) : ordered
  const items = older.slice(-limit)
  const first = items[0]
  return { items, olderCursor: older.length > items.length && first ? encodeCursor(first) : null }
}

export type { MessagePage }
