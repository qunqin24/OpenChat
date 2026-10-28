import { useTranslation } from 'react-i18next'

import { useSubagents } from '@/hooks/use-subagents'
import { useSubagentPanel } from '@/stores/subagent-panel-store'

import { SubagentsIcon } from './SubagentAvatar'

/**
 * The conversation's sub-agents, from the top bar beside 写作: the panel at
 * a click, whenever the conversation has had one - not only while one works,
 * as the summary above the composer shows - so a finished one is not found
 * by scrolling back to the reply that launched it.
 */
export function SubagentsButton({ conversationId }: { conversationId: string }) {
  const { t } = useTranslation()
  const list = useSubagents(conversationId, true).data?.items ?? []
  const { open } = useSubagentPanel()
  if (list.length === 0) return null
  return (
    <button type="button" className="n-temp-toggle n-docs-toggle" onClick={() => open(conversationId)} aria-label={t('subagents.open')}>
      <SubagentsIcon />
      <span>{t('subagents.summary.label')}</span>
      {list.length > 1 && <span className="n-docs-count">{list.length}</span>}
    </button>
  )
}

export default SubagentsButton
