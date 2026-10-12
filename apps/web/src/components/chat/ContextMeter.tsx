import { useQuery } from '@tanstack/react-query'
import { Minimize2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from '@/components/ui/dropdown-menu'
import { useCompact, useCompaction } from '@/hooks/use-compaction'
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

interface ContextMeterProps {
  conversationId: string
  limit?: number | null
  /** The conversation is answering: it cannot be compressed from under the run. */
  busy?: boolean
}

/**
 * How full the model's context window is, as a ring beside the model picker.
 * The figure is what the conversation's latest request actually took, as the
 * provider counted it — system prompt, tools and attachments included — set
 * against the window of the model chosen now, which the next request goes to.
 * Nothing shows until something has been answered, or for a model whose window
 * is unknown.
 *
 * It opens to the same figures and to "compress now": the older conversation
 * is summarised and the model is sent the summary instead. The ring does not
 * move until the next request, which is the one that takes less.
 */
export default function ContextMeter({ conversationId, limit, busy }: ContextMeterProps) {
  const { t } = useTranslation()
  const usage = useQuery({
    queryKey: queryKeys.conversations.context(conversationId),
    queryFn: async ({ signal }) =>
      unwrap(await api.GET('/conversations/{conversationId}/context', { params: { path: { conversationId } }, signal })),
  })
  const summary = useCompaction(conversationId)
  const compress = useCompact(conversationId)
  const used = usage.data?.usedTokens
  if (used == null || !limit) return null

  const share = Math.min(used / limit, 1)
  const percent = Math.round(share * 100)
  const level = share >= 0.95 ? 'is-full' : share >= 0.8 ? 'is-high' : undefined
  return (
    <DropdownMenu>
      <Tooltip
        label={t('chat.context.label')}
        hint={t('chat.context.hint', { percent, left: 100 - percent, used: compact(used), limit: compact(limit) })}
        disabled={compress.isPending}
      >
        <DropdownMenuTrigger asChild>
          <button
            type="button"
            className={cn('n-context-meter', level, compress.isPending && 'is-busy')}
            aria-label={t('chat.context.aria', { percent })}
            data-motion={compress.isPending ? 'essential' : undefined}
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
        </DropdownMenuTrigger>
      </Tooltip>
      <DropdownMenuContent className="n-context-menu" align="end" side="top">
        <div className="n-context-info">
          <span className="n-context-title">{t('chat.context.label')}</span>
          <span className="n-context-figures">
            {t('chat.context.hint', { percent, left: 100 - percent, used: compact(used), limit: compact(limit) })}
          </span>
          {summary && <span className="n-context-note">{t('chat.context.compacted')}</span>}
        </div>
        <DropdownMenuItem
          disabled={busy || compress.isPending}
          onSelect={() => compress.mutate(undefined, { onSuccess: () => toast.success(t('chat.compaction.done')) })}
        >
          <Minimize2 aria-hidden="true" />
          <span>{compress.isPending ? t('chat.context.compacting') : t('chat.context.compact')}</span>
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  )
}
