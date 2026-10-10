import { byRow } from './transcript'
import type { StoredConversation, StoredMessage, StoredRun } from './types'

/**
 * Data kept by a version that had branches.
 *
 * A conversation used to be a tree: every user message named the one it
 * followed (`parentInputId`), the conversation pointed at the end of the branch
 * shown (`currentInputId`), a regeneration set the answers it replaced aside
 * (`superseded`), and an edit was a sibling of the question it reworded
 * (`editOf`). Now what is replaced is deleted, and a conversation is its rows
 * in order. Data kept the old way is brought to the new when it is first read,
 * by keeping exactly what the reader saw - the inputs on the path to the
 * current leaf and the rows of each one's answer that was not set aside - and
 * deleting the rest. It is the server's 20261020000001 migration, over the
 * browser's own storage.
 */

type Old<T> = T & {
  parentInputId?: string | null
  shownAt?: number | null
  editOf?: string
  versions?: string[]
  branches?: string[]
  superseded?: boolean
  currentInputId?: string | null
}

/** Whether a conversation was kept by a version with branches: it has a current leaf (null for one with no questions yet). */
export const isBranched = (c: StoredConversation) => 'currentInputId' in c

export interface Pruned {
  conversation: StoredConversation
  /** The rows that stay, without the fields of the tree. */
  messages: StoredMessage[]
  runs: StoredRun[]
  /** What goes. */
  deletedMessages: string[]
  deletedRuns: string[]
  /** Files that no row that stays names: their bytes go too. */
  deletedFiles: string[]
}

/** The inputs on the path from the root to the current leaf (conversation_path, reversed). */
function pathOf(c: Old<StoredConversation>, messages: Old<StoredMessage>[]): Set<string> {
  const byId = new Map(messages.map((m) => [m.id, m]))
  const path = new Set<string>()
  let at = c.currentInputId ? byId.get(c.currentInputId) : undefined
  // A chain that looped would end here, as the UNION in the SQL ends it.
  while (at && !path.has(at.id)) {
    path.add(at.id)
    at = at.parentInputId ? byId.get(at.parentInputId) : undefined
  }
  return path
}

export function pruneBranches(c: StoredConversation, held: StoredMessage[], heldRuns: StoredRun[]): Pruned {
  const messages = held as Old<StoredMessage>[]
  const runs = heldRuns as Old<StoredRun>[]
  const onPath = pathOf(c as Old<StoredConversation>, messages)
  const answers = new Set(runs.filter((r) => onPath.has(r.inputId) && !r.superseded).map((r) => r.id))
  const shown = (m: Old<StoredMessage>) => (m.role === 'user' ? onPath.has(m.id) : Boolean(m.runId) && answers.has(m.runId!))

  const keptMessages = messages.filter(shown).sort(byRow)
  const keptIds = new Set(keptMessages.map((m) => m.id))
  const keptRuns = runs.filter((r) => answers.has(r.id))
  const keptRunIds = new Set(keptRuns.map((r) => r.id))
  const files = new Set(keptMessages.flatMap((m) => m.attachments?.map((f) => f.id) ?? []))

  const strip = <T extends object>(row: T, fields: string[]): T => {
    const next = { ...row } as Record<string, unknown>
    for (const field of fields) delete next[field]
    return next as T
  }
  const { currentInputId: _leaf, ...conversation } = c as Old<StoredConversation>
  void _leaf
  return {
    conversation: conversation as StoredConversation,
    messages: keptMessages.map((m) => strip(m, ['parentInputId', 'shownAt', 'editOf', 'versions', 'branches'])),
    runs: keptRuns.map((r) => strip(r, ['superseded'])),
    deletedMessages: messages.filter((m) => !keptIds.has(m.id)).map((m) => m.id),
    deletedRuns: runs.filter((r) => !keptRunIds.has(r.id)).map((r) => r.id),
    deletedFiles: [
      ...new Set(messages.filter((m) => !keptIds.has(m.id)).flatMap((m) => m.attachments?.map((f) => f.id) ?? [])),
    ].filter((id) => !files.has(id)),
  }
}
