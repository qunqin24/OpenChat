import type { ToolResultPart } from '@openchat/api-client'

/** The tool the model keeps its notes about the user with. */
export const MEMORY_TOOL = 'memory'

/** What a succeeded call of the memory tool changed (the server's MemoryDetails). */
export interface MemoryChange {
  op: 'add' | 'update' | 'delete'
  content: string
  before?: string
  eventId: string
}

/** The change a result records, or null for a failed call or one with nothing recorded. */
export function memoryChangeOf(result: ToolResultPart | undefined): MemoryChange | null {
  if (result?.status !== 'succeeded') return null
  const d = result.details
  if (!d || typeof d !== 'object') return null
  const op = d.op
  if ((op !== 'add' && op !== 'update' && op !== 'delete') || typeof d.eventId !== 'string') return null
  return {
    op,
    eventId: d.eventId,
    content: typeof d.content === 'string' ? d.content : '',
    ...(typeof d.before === 'string' && d.before ? { before: d.before } : {}),
  }
}
