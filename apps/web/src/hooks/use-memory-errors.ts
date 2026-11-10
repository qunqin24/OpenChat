import { useTranslation } from 'react-i18next'

import { memoryErrorText, saveRefusalText, undoErrorText } from '@openchat/chat-core/memory-errors'

// The wording is chat-core's (memory-errors.ts), shared with the app; these
// only supply the reader's language.

/** The words for a save that content moderation turned away, or null when that is not why it failed. */
export function useSaveRefusalText() {
  const { t } = useTranslation()
  return (error: unknown) => saveRefusalText(t, error)
}

/** The words for a failed write to the memory list. */
export function useMemoryErrorText() {
  const { t } = useTranslation()
  return (error: unknown) => memoryErrorText(t, error)
}

/** The words for an undo that could not be made. */
export function useUndoErrorText() {
  const { t } = useTranslation()
  return (error: unknown) => undoErrorText(t, error)
}
