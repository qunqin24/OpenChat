import { useTranslation } from 'react-i18next'

import { displayTitle } from '@openchat/chat-core/titles'

/** `displayTitle` (chat-core) bound to the current language. */
export function useDisplayTitle(): (title: string | null | undefined) => string {
  const { t } = useTranslation()
  return (title) => displayTitle(title, t)
}
