import { useQuery } from '@tanstack/react-query'
import { Popover } from 'radix-ui'
import { useTranslation } from 'react-i18next'

import { useCompaction } from '@/hooks/use-compaction'
import { api, unwrap } from '@/lib/api'
import { queryKeys } from '@openchat/chat-core/query-keys'
import { cn } from '@/lib/utils'
import Tooltip from './Tooltip'

/** 32k, 258k, 1.2M: the window sizes people know models by. */
function compact(tokens: number): string {
  if (tokens >= 1_000_000) return `${+(tokens / 1_000_000).toFixed(1)}M`
  if (tokens >= 1_000) return `${Math.round(tokens / 1_000)}k`
  return String(tokens)
}

const RADIUS = 7
const CIRCUMFERENCE = 2 * Math.PI * RADIUS

interface ContextMeterProps {
  conversationId: string
  limit?: number | null
}

/**
 * How full the model's context window is, as a ring beside the model picker.
 * The figure is what the conversation's latest request actually took, as the
 * provider counted it — system prompt, tools and attachments included — set
 * against the window of the model chosen now, which the next request goes to.
 * Nothing shows until something has been answered, or for a model whose window
 * is unknown.
 *
 * It opens to the same figures and notes, drawn like its tooltip (the dark
 * chip with an arrow) rather than as a menu. Compressing is only ever a run's
 * doing, so there is nothing to press: once a run has written a summary, the
 * server estimates the compressed context until the next request reports what
 * it took (`estimated`), so the ring drops as soon as the summary is written.
 */
export default function ContextMeter({ conversationId, limit }: ContextMeterProps) {
  const { t } = useTranslation()
  const usage = useQuery({
    queryKey: queryKeys.conversations.context(conversationId),
    queryFn: async ({ signal }) =>
      unwrap(await api.GET('/conversations/{conversationId}/context', { params: { path: { conversationId } }, signal })),
  })
  const summary = useCompaction(conversationId)
  const used = usage.data?.usedTokens
  if (used == null || !limit) return null

  const share = Math.min(used / limit, 1)
  const percent = Math.round(share * 100)
  const level = share >= 0.95 ? 'is-full' : share >= 0.8 ? 'is-high' : undefined
  const hint = t('chat.context.hint', { percent, left: 100 - percent, used: compact(used), limit: compact(limit) })
  return (
    <Popover.Root>
      <Tooltip label={t('chat.context.label')} hint={hint}>
        <Popover.Trigger asChild>
          <button
            type="button"
            className={cn('n-context-meter', level)}
            aria-label={t('chat.context.aria', { percent })}
          >
            <svg viewBox="0 0 18 18" aria-hidden="true">
              <circle className="n-context-track" cx="9" cy="9" r={RADIUS} />
              <circle
                className="n-context-fill"
                cx="9"
                cy="9"
                r={RADIUS}
                strokeDasharray={CIRCUMFERENCE}
                strokeDashoffset={CIRCUMFERENCE * (1 - share)}
              />
            </svg>
          </button>
        </Popover.Trigger>
      </Tooltip>
      <Popover.Portal>
        <Popover.Content className="n-tip" side="top" sideOffset={8} collisionPadding={10}>
          <span className="n-tip-label">{t('chat.context.label')}</span>
          <span className="n-tip-hint">{hint}</span>
          {usage.data?.estimated && <span className="n-tip-hint n-context-note">{t('chat.context.estimated')}</span>}
          {summary && <span className="n-tip-hint n-context-note">{t('chat.context.compacted')}</span>}
          <Popover.Arrow className="n-tip-arrow" width={11} height={5} />
        </Popover.Content>
      </Popover.Portal>
    </Popover.Root>
  )
}
