import { isOpenChatError } from '@openchat/api-client'
import { useTranslation } from 'react-i18next'

import { useSaveRefusalText } from './use-memory-errors'

/**
 * The words for a failed write of a skill: a refusal by content moderation as
 * for the custom instructions, a format problem by its code (the server's own
 * sentence, which names the file or field, after it), or a generic failure.
 */
export function useSkillErrorText() {
  const { t } = useTranslation()
  const refused = useSaveRefusalText()
  return (error: unknown) => {
    const refusal = refused(error)
    if (refusal) return refusal
    if (!isOpenChatError(error)) return t('settings.skills.error.failed')
    switch (error.code) {
      case 'skill_name_taken':
      case 'skill_limit':
      case 'personal_skills_off':
      case 'file_too_large':
        return t(`settings.skills.error.${error.code}`)
    }
    if (error.code.startsWith('skill_')) return t('settings.skills.error.format', { detail: error.message })
    return t('settings.skills.error.failed')
  }
}
