import { GuestError } from './errors'
import type { Memory, MemoryEvent } from './types'

/**
 * A guest's memory list, with the rules the server applies to an account's
 * (internal/memory): a note is one line of at most 200 characters, there are at
 * most a hundred, a duplicate is refused, what the model changed is logged for
 * an undo and what the person changed is not, and the undo of a change is only
 * possible while the note still reads as the change left it. The functions are
 * pure - they take the list and return the next one - and the repository
 * stores the result.
 */

export const MAX_CONTENT = 200
export const MAX_MEMORIES = 100
export const MAX_INSTRUCTIONS = 1500
/** How long the log of what the model changed is kept. */
export const EVENT_RETENTION_MS = 30 * 24 * 3600 * 1000

const refuse = (status: number, code: string, message: string, field?: string) =>
  new GuestError(status, code, message, field)

/** A note as it is kept: whitespace collapsed to single spaces, and checked. */
export function cleanMemory(raw: string): string {
  const content = raw.trim().split(/\s+/).filter(Boolean).join(' ')
  if (content === '') throw refuse(400, 'memory_empty', 'a memory cannot be empty', 'content')
  if ([...content].length > MAX_CONTENT) {
    throw refuse(400, 'memory_too_long', `a memory is at most ${MAX_CONTENT} characters`, 'content')
  }
  return content
}

const same = (a: string, b: string) => a.toLocaleLowerCase() === b.toLocaleLowerCase()

function checkRoom(list: Memory[], content: string, except?: string) {
  if (!except && list.length >= MAX_MEMORIES) {
    throw refuse(409, 'memory_full', `the memory list is full (${MAX_MEMORIES})`)
  }
  if (list.some((m) => m.id !== except && same(m.content, content))) {
    throw refuse(409, 'memory_duplicate', 'that is already remembered')
  }
}

/** Oldest first, as the server lists them. */
export function sortMemories(list: Memory[]): Memory[] {
  return [...list].sort((a, b) => (a.createdAt < b.createdAt ? -1 : a.createdAt > b.createdAt ? 1 : a.id < b.id ? -1 : 1))
}

export function addMemory(list: Memory[], raw: string, id: string, now: Date): { memory: Memory } {
  const content = cleanMemory(raw)
  checkRoom(list, content)
  const at = now.toISOString()
  return { memory: { id, content, source: 'user', conversationId: null, createdAt: at, updatedAt: at } }
}

export function rewriteMemory(list: Memory[], id: string, raw: string, now: Date): Memory {
  const content = cleanMemory(raw)
  const current = list.find((m) => m.id === id)
  if (!current) throw refuse(404, 'not_found', 'resource not found')
  checkRoom(list, content, id)
  // The server's receipt was for the words that were there (receipts.ts).
  // eslint-disable-next-line @typescript-eslint/no-unused-vars
  const { receipt: _receipt, ...rest } = current
  return { ...rest, content, updatedAt: now.toISOString() }
}

/** The log entries that stay once a person has changed or removed a note themselves: its own are forgotten. */
export function forgetEvents(events: MemoryEvent[], memoryId: string): MemoryEvent[] {
  return events.filter((e) => e.memoryId !== memoryId)
}

/** The log as listed: the last thirty days, newest first. */
export function recentEvents(events: MemoryEvent[], now: Date, limit = 50): MemoryEvent[] {
  const cutoff = now.getTime() - EVENT_RETENTION_MS
  return events
    .filter((e) => Date.parse(e.createdAt) > cutoff)
    .sort((a, b) => (a.createdAt < b.createdAt ? 1 : a.createdAt > b.createdAt ? -1 : a.id < b.id ? 1 : -1))
    .slice(0, limit)
}

export interface UndoResult {
  /** The list after the undo. */
  list: Memory[]
  event: MemoryEvent
}

/**
 * Reverses a change the model made: an added note is removed, an edited one put
 * back, a deleted one restored. Only while the note still reads as the change
 * left it - an edit made since is the person's, and wins.
 */
export function undoEvent(list: Memory[], events: MemoryEvent[], eventId: string, now: Date): UndoResult {
  const event = events.find((e) => e.id === eventId)
  if (!event) throw refuse(404, 'not_found', 'resource not found')
  if (event.undoneAt) throw refuse(409, 'already_undone', 'already undone')
  const current = list.find((m) => m.id === event.memoryId)
  const changed = () => refuse(409, 'memory_changed', 'the memory was changed since')
  let next = list

  switch (event.op) {
    case 'add':
      // Gone already is as good as undone.
      if (current) {
        if (event.after == null || current.content !== event.after) throw changed()
        next = list.filter((m) => m.id !== current.id)
      }
      break
    case 'update':
      if (!current) throw refuse(404, 'not_found', 'resource not found')
      if (event.after == null || event.before == null || current.content !== event.after) throw changed()
      next = list.map((m) => {
        if (m.id !== current.id) return m
        // eslint-disable-next-line @typescript-eslint/no-unused-vars
        const { receipt: _receipt, ...rest } = m
        return { ...rest, content: event.before as string, updatedAt: now.toISOString() }
      })
      break
    case 'delete': {
      if (current) throw changed() // written back already
      if (event.before == null) throw refuse(404, 'not_found', 'resource not found')
      checkRoom(list, event.before)
      const at = now.toISOString()
      // Restored by a person's decision, so it is theirs now.
      next = [
        ...list,
        { id: event.memoryId, content: event.before, source: 'user', conversationId: event.conversationId ?? null, createdAt: at, updatedAt: at },
      ]
      break
    }
  }
  return { list: next, event: { ...event, undoneAt: now.toISOString() } }
}

export interface ModelChanges {
  /** The notes to write: the model's additions and rewrites. */
  put: Memory[]
  /** The notes to remove: the ones the model deleted. */
  remove: string[]
  /** The log entries of the changes that were made, for the undo list. */
  events: MemoryEvent[]
}

/**
 * Applies what the model changed during a run to the list as it is *now*.
 *
 * The server's list at the end of a run is the list the run began with, plus
 * the model's changes. The guest's own list may have moved since - a note
 * written, rewritten or deleted in the settings while the answer streamed - and
 * replacing it with the server's would delete the new note and bring back the
 * deleted one. So only the model's changes (the events) are applied, each only
 * if the note is still as the model found it: a note the person has rewritten
 * or removed since is theirs and stays so, and the change is dropped along with
 * its log entry (there is nothing to undo).
 *
 * `theirs` is the server's list, for the notes' details (when, from where).
 */
export function applyModelChanges(list: Memory[], events: MemoryEvent[], theirs: Memory[]): ModelChanges {
  const working = new Map(list.map((m) => [m.id, m]))
  const put = new Map<string, Memory>()
  const remove = new Set<string>()
  const applied: MemoryEvent[] = []
  const server = new Map(theirs.map((m) => [m.id, m]))
  const ordered = [...events].sort((a, b) =>
    a.createdAt < b.createdAt ? -1 : a.createdAt > b.createdAt ? 1 : a.id < b.id ? -1 : 1,
  )
  const taken = (content: string, except: string) =>
    [...working.values()].some((m) => m.id !== except && same(m.content, content))

  for (const event of ordered) {
    const current = working.get(event.memoryId)
    const wrote = server.get(event.memoryId)
    switch (event.op) {
      case 'add': {
        const content = wrote?.content ?? event.after
        if (current) {
          // Delivered before: already there as the model left it.
          if (event.after != null && current.content === event.after) applied.push(event)
          break
        }
        if (content == null || working.size >= MAX_MEMORIES || taken(content, event.memoryId)) break
        const at = event.createdAt
        const memory: Memory = wrote
          ? { ...wrote, conversationId: wrote.conversationId ?? null }
          : {
              id: event.memoryId,
              content,
              source: 'model',
              conversationId: event.conversationId ?? null,
              createdAt: at,
              updatedAt: at,
            }
        working.set(memory.id, memory)
        put.set(memory.id, memory)
        remove.delete(memory.id)
        applied.push(event)
        break
      }
      case 'update': {
        // Gone, or rewritten by the person since: theirs.
        if (!current || event.after == null) break
        if (event.before != null && current.content !== event.before) {
          if (current.content === event.after) applied.push(event)
          break
        }
        if (taken(event.after, current.id)) break
        // eslint-disable-next-line @typescript-eslint/no-unused-vars
        const { receipt: _stale, ...unsigned } = current
        const memory: Memory = {
          ...unsigned,
          content: event.after,
          updatedAt: wrote?.updatedAt ?? event.createdAt,
          ...(wrote ? { source: wrote.source } : {}),
          // The server signed the words it wrote.
          ...(wrote?.receipt && wrote.content === event.after ? { receipt: wrote.receipt } : {}),
        }
        working.set(memory.id, memory)
        put.set(memory.id, memory)
        applied.push(event)
        break
      }
      case 'delete': {
        if (!current) break
        if (event.before != null && current.content !== event.before) break
        working.delete(current.id)
        put.delete(current.id)
        remove.add(current.id)
        applied.push(event)
        break
      }
    }
  }
  return { put: [...put.values()], remove: [...remove], events: applied }
}
