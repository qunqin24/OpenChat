import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'

import { api, unwrap } from '@/lib/api'
import { queryKeys } from '@/lib/query-keys'
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

/**
 * How full the model's context window is, as a ring beside the model picker.
 * The figure is what the conversation's latest request actually took, as the
 * provider counted it — system prompt, tools and attachments included — set
 * against the window of the model chosen now, which the next request goes to.
 * Nothing shows until something has been answered, or for a model whose window
 * is unknown.
 */
export default function ContextMeter({ conversationId, limit }: { conversationId: string; limit?: number | null }) {
  const { t } = useTranslation()
  const usage = useQuery({
    queryKey: queryKeys.conversations.context(conversationId),
    queryFn: async ({ signal }) =>
      unwrap(await api.GET('/conversations/{conversationId}/context', { params: { path: { conversationId } }, signal })),
  })
  const used = usage.data?.usedTokens
  if (used == null || !limit) return null

  const share = Math.min(used / limit, 1)
  const percent = Math.round(share * 100)
  const level = share >= 0.95 ? 'is-full' : share >= 0.8 ? 'is-high' : undefined
  return (
    <Tooltip
      label={t('next.context.label')}
      hint={t('next.context.hint', { percent, left: 100 - percent, used: compact(used), limit: compact(limit) })}
    >
      <span className={cn('n-context-meter', level)} role="img" tabIndex={0} aria-label={t('next.context.aria', { percent })}>
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
      </span>
    </Tooltip>
  )
}
