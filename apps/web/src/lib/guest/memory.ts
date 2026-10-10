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
  return { ...current, content, updatedAt: now.toISOString() }
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
      next = list.map((m) => (m.id === current.id ? { ...m, content: event.before as string, updatedAt: now.toISOString() } : m))
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
