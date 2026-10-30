import { useTranslation } from 'react-i18next'

import { memoryErrorCode } from '@openchat/chat-core/memory-errors'
import { saveRefusalOf } from '@openchat/chat-core/moderation-refusal'

/**
 * The words for a save that content moderation turned away, or null when that is
 * not why it failed: the wording family of a refused message ("did not pass
 * content moderation"), with the strike warning when the server gives one.
 * Nothing was saved, so the text stays where the person typed it.
 */
export function useSaveRefusalText() {
  const { t } = useTranslation()
  return (error: unknown): string | null => {
    const refusal = saveRefusalOf(error)
    if (!refusal) return null
    if (refusal.kind === 'unavailable') return t('settings.refused.unavailable')
    const warning = refusal.strikesLeft
      ? ` ${t('chat.refused.warning', { count: refusal.strikesLeft, hours: refusal.windowHours })}`
      : ''
    return t('settings.refused.blocked') + warning
  }
}

/** The words for a failed write to the memory list. */
export function useMemoryErrorText() {
  const { t } = useTranslation()
  const refused = useSaveRefusalText()
  return (error: unknown) => {
    const refusal = refused(error)
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
}

/** The words for an undo that could not be made. */
export function useUndoErrorText() {
  const { t } = useTranslation()
  return (error: unknown) => {
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
}
