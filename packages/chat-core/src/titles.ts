import type { TFunction } from 'i18next'

/**
 * What the server (internal/api/titles.go `defaultTitle`) calls a
 * conversation until it is titled. It is stored in English whatever the
 * reader's language, so it is never shown as it is: `displayTitle` swaps it for
 * the translated name. A conversation the
 * reader renamed to exactly this is shown the same way - it is the same words.
 */
export const DEFAULT_TITLE = 'New chat'

/** A conversation's title as it is read, in the reader's language. The one place that decides it. */
export function displayTitle(title: string | null | undefined, t: TFunction): string {
  const named = title?.trim()
  return !named || named === DEFAULT_TITLE ? t('chat.untitled') : named
}
