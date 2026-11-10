import { isOpenChatError } from '@openchat/api-client'
import type { TFunction } from 'i18next'

import { saveRefusalOf } from './moderation-refusal'

type T = TFunction<'common'>

/** Server error codes of the memory endpoints that have their own wording. */
const CODES = ['memory_full', 'memory_duplicate', 'memory_too_long', 'memory_empty', 'memory_changed', 'already_undone'] as const
export type MemoryErrorCode = (typeof CODES)[number]

/** The code of a failed memory call, if it is one the interface words itself. */
export function memoryErrorCode(error: unknown): MemoryErrorCode | undefined {
  return isOpenChatError(error) ? CODES.find((code) => code === error.code) : undefined
}

/**
 * The words for a save that content moderation turned away, or null when that is
 * not why it failed: the wording family of a refused message ("did not pass
 * content moderation"), with the strike warning when the server gives one.
 * Nothing was saved, so the text stays where the person typed it.
 */
export function saveRefusalText(t: T, error: unknown): string | null {
  const refusal = saveRefusalOf(error)
  if (!refusal) return null
  if (refusal.kind === 'unavailable') return t('settings.refused.unavailable')
  const warning = refusal.strikesLeft
    ? ` ${t('chat.refused.warning', { count: refusal.strikesLeft, hours: refusal.windowHours })}`
    : ''
  return t('settings.refused.blocked') + warning
}

/** The words for a failed write to the memory list. */
export function memoryErrorText(t: T, error: unknown): string {
  const refusal = saveRefusalText(t, error)
  if (refusal) return refusal
  switch (memoryErrorCode(error)) {
    case 'memory_full':
      return t('settings.memory.full-error')
    case 'memory_duplicate':
      return t('settings.memory.duplicate')
    case 'memory_too_long':
      return t('settings.memory.too-long')
    case 'memory_empty':
      return t('settings.memory.empty-error')
    default:
      return t('settings.memory.failed')
  }
}

/** The words for an undo that could not be made. */
export function undoErrorText(t: T, error: unknown): string {
  switch (memoryErrorCode(error)) {
    case 'memory_changed':
      return t('settings.memory.undo-changed')
    case 'memory_full':
      return t('settings.memory.full-error')
    case 'memory_duplicate':
      return t('settings.memory.duplicate')
    default:
      return t('settings.memory.undo-failed')
  }
}
