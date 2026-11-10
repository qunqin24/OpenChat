import type { ToolResultPart } from '@openchat/api-client'
import type { TFunction } from 'i18next'

// The chat_history tool in a reply's activity (docs/design/chat-history-plan.md
// §4.5): the model searches or reads the person's earlier conversations. The
// line says which, with the keywords of a search; the web lists the
// conversations a search found, each a link. Pure, so both apps read the
// arguments and details the same way.

export const CHAT_HISTORY_TOOL = 'chat_history'

type T = TFunction<'common'>

/** One conversation a search found (the result's `details.hits`). */
export interface HistoryHit {
  conversationId: string
  /** Absent when the conversation has no checked title. */
  title?: string
  at?: string
}

/** The search words of a call, as far as its arguments have arrived. */
export function historyKeywords(args: Record<string, unknown>): string[] {
  if (!Array.isArray(args.keywords)) return []
  return args.keywords.filter((word): word is string => typeof word === 'string' && word.trim() !== '').map((word) => word.trim())
}

/**
 * The conversations a call's result names: a search's hits, or the one
 * conversation a read opened. Empty for a call that failed or is still going.
 */
export function historyHitsOf(result?: Pick<ToolResultPart, 'status' | 'details'>): HistoryHit[] {
  if (result?.status !== 'succeeded') return []
  const details = result.details as { op?: unknown; hits?: unknown; conversationId?: unknown; title?: unknown } | undefined
  if (!details || typeof details !== 'object') return []
  const one = (value: unknown): HistoryHit | null => {
    if (typeof value !== 'object' || value === null) return null
    const hit = value as Record<string, unknown>
    if (typeof hit.conversationId !== 'string' || hit.conversationId === '') return null
    return {
      conversationId: hit.conversationId,
      ...(typeof hit.title === 'string' && hit.title !== '' ? { title: hit.title } : {}),
      ...(typeof hit.at === 'string' ? { at: hit.at } : {}),
    }
  }
  if (details.op === 'read') {
    const hit = one(details)
    return hit ? [hit] : []
  }
  if (!Array.isArray(details.hits)) return []
  return details.hits.map(one).filter((hit): hit is HistoryHit => hit !== null)
}

/** The step's line: what the call is doing, or did; a search names its keywords. */
export function historyStepLabel(t: T, args: Record<string, unknown>, running: boolean): string {
  if (args.op === 'read') return t(running ? 'chat.tool.history-reading' : 'chat.tool.history-read')
  const query = historyKeywords(args).join(' ')
  if (!query) return t(running ? 'chat.tool.history-searching' : 'chat.tool.history-search')
  return t(running ? 'chat.tool.history-searching-for' : 'chat.tool.history-search-for', { query })
}
