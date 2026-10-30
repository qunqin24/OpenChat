import { useTranslation } from 'react-i18next'

import { useSubagents } from '@/hooks/use-subagents'
import { showSummary, tally } from '@openchat/chat-core/subagents'
import { useSubagentPanel } from '@/stores/subagent-panel-store'
import { SubagentAvatars } from './SubagentAvatar'

/** How many working sub-agents' marks the summary shows. */
const SHOWN = 3

/**
 * 「子智能体 · 1 个运行中 · 2 完成」: just above the composer, on every
 * screen. Up while one works, and after the
 * panel was opened until it is closed; a click opens the panel.
 */
export function SubagentSummary({ conversationId }: { conversationId: string }) {
  const { t } = useTranslation()
  const list = useSubagents(conversationId, true).data?.items ?? []
  const { open, seen, conversationId: openFor } = useSubagentPanel()
  // The panel already shows it all beside the chat.
  if (openFor === conversationId || !showSummary(list, seen === conversationId)) return null
  const { running, queued, finished } = tally(list)
  return (
    <button
      type="button"
      className="n-subagent-summary"
      onClick={() => open(conversationId)}
      aria-label={t('subagents.open')}
    >
      <span className="n-subagent-summary-label">{t('subagents.summary.label')}</span>
      {running.length > 0 && (
        <>
          <span className="n-subagent-summary-dot" aria-hidden="true">·</span>
          <SubagentAvatars ids={running.slice(0, SHOWN).map((a) => a.id)} state={() => 'running'} />
          <span>{t('subagents.summary.running', { count: running.length })}</span>
        </>
      )}
      {queued.length > 0 && (
        <>
          <span className="n-subagent-summary-dot" aria-hidden="true">·</span>
          <span>{t('subagents.summary.queued', { count: queued.length })}</span>
        </>
      )}
      {finished.length > 0 && (
        <>
          <span className="n-subagent-summary-dot" aria-hidden="true">·</span>
          <span>{t('subagents.summary.done', { count: finished.length })}</span>
        </>
      )}
    </button>
  )
}

export default SubagentSummary
