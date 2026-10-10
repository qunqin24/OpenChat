import { isOpenChatError } from '@openchat/api-client'

/** Server error codes of the memory endpoints that have their own wording. */
const CODES = ['memory_full', 'memory_duplicate', 'memory_too_long', 'memory_empty', 'memory_changed', 'already_undone'] as const
export type MemoryErrorCode = (typeof CODES)[number]

/** The code of a failed memory call, if it is one the interface words itself. */
export function memoryErrorCode(error: unknown): MemoryErrorCode | undefined {
  return isOpenChatError(error) ? CODES.find((code) => code === error.code) : undefined
}
