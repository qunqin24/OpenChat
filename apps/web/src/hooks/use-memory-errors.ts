import { useTranslation } from 'react-i18next'

import { memoryErrorCode } from '@/lib/memory-errors'

/** The words for a failed write to the memory list. */
export function useMemoryErrorText() {
  const { t } = useTranslation()
  return (error: unknown) => {
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
