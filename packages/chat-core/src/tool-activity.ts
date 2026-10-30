import type { ToolCallPart, ToolResultPart } from '@openchat/api-client'
import type { TFunction } from 'i18next'

import { isHttpUrl, siteName } from './urls'

// A reply's process as its line tells it (the web's ToolActivity.tsx, the
// app's ActivityLine; what the process is, reply-process.ts): what each step
// is called, and the one line that sums it up. Pure, so both apps say the same thing.

export type ToolStatus = 'pending' | 'running' | ToolResultPart['status']

export interface ToolActivityStep {
  call: ToolCallPart
  result?: ToolResultPart
  status: ToolStatus
}

/** What the model thought, or said on its way, in the process. */
export interface ActivityNote {
  kind: 'note' | 'reasoning'
  text: string
  /** Still being written: shown as an answer's text is, each batch fading in. */
  streaming?: boolean
  /** Began while the reader watched, so its text fades in as it arrives. */
  fresh?: boolean
  /** How long the model thought, for reasoning: the line sums it up ahead of the steps. */
  durationMs?: number
}

/** One entry of the activity, in the order it happened. */
export type ActivityItem = ({ kind: 'tool' } & ToolActivityStep | ActivityNote) & { key: string }

/**
 * The open process as ChatGPT lays it out: what the model said on its way,
 * shown as it said it, and between those words each run of thinking and steps
 * folded into a line of its own (`run`), opened on demand.
 */
export type ActivitySegment = { kind: 'note'; item: ActivityNote & { key: string } } | { kind: 'run'; key: string; items: ActivityItem[] }

/**
 * The items parted at what the model said. Null when it said nothing on its
 * way: then the process is one run, and a line of its own inside the fold
 * would only say again what the fold's line says.
 */
export function activitySegments(items: ActivityItem[]): ActivitySegment[] | null {
  if (!items.some((item) => item.kind === 'note')) return null
  const segments: ActivitySegment[] = []
  for (const item of items) {
    if (item.kind === 'note') {
      segments.push({ kind: 'note', item })
      continue
    }
    const last = segments[segments.length - 1]
    if (last?.kind === 'run') last.items.push(item)
    else segments.push({ kind: 'run', key: item.key, items: [item] })
  }
  return segments
}

export interface SearchHit {
  title: string
  url: string
}

type T = TFunction<'common'>

/** The periods a search can be limited to. */
const RECENCIES = ['day', 'week', 'month', 'year'] as const
const isRecency = (value: unknown): value is (typeof RECENCIES)[number] => RECENCIES.some((r) => r === value)

export function stepLabel(t: T, step: ToolActivityStep, running: boolean): string {
  const args = callArguments(step.call)
  if (step.call.name === 'web_search') {
    const query = typeof args.query === 'string' ? args.query : ''
    if (!query) return t('chat.tool.search-pending')
    // The period and the sites it was held to, after the words (tools/websearch.go).
    const limits: string[] = []
    if (isRecency(args.recency)) limits.push(t(`chat.tool.recency.${args.recency}`))
    if (Array.isArray(args.sites)) limits.push(...args.sites.filter((site): site is string => typeof site === 'string'))
    const label = [query, ...limits].join(' · ')
    return running ? t('chat.tool.searching', { query: label }) : label
  }
  if (step.call.name === 'web_fetch') {
    const url = typeof args.url === 'string' ? args.url : ''
    if (!url) return t('chat.tool.fetch-pending')
    const host = siteName(url)
    // Looking for something in a page, or reading on in a long one (tools/webfetch.go).
    const find = typeof args.find === 'string' ? args.find.trim() : ''
    if (find) return running ? t('chat.tool.finding', { host, find }) : t('chat.tool.find', { host, find })
    if (typeof args.offset === 'number' && args.offset > 0) {
      return running ? t('chat.tool.reading-on', { host }) : t('chat.tool.read-on', { host })
    }
    return running ? t('chat.tool.reading', { host }) : t('chat.tool.fetch', { host })
  }
  if (isFileTool(step.call.name)) return fileStepLabel(t, step, args, running)
  if (step.call.name === 'memory') return t('chat.memory.step')
  if (step.call.name === 'scheduled_tasks') return running ? t('tasks.step-running') : t('tasks.step')
  if (step.call.name === 'document') return documentStepLabel(t, step, args, running)
  if (step.call.name.startsWith('subagent')) return subagentStepLabel(t, step, args, running)
  return t('chat.tool.generic', { name: step.call.name })
}

/** file_read and file_search: the tools that read a long text file the user attached (agent/files.go). */
export function isFileTool(name: string) {
  return name === 'file_read' || name === 'file_search'
}

/**
 * A read names the file and the lines its result says it read; a search, its
 * phrase, the file when it named one, and how many lines matched.
 */
function fileStepLabel(t: T, step: ToolActivityStep, args: Record<string, unknown>, running: boolean): string {
  const details = (step.result?.status === 'succeeded' ? step.result.details : undefined) as
    | { filename?: unknown; startLine?: unknown; endLine?: unknown; matches?: unknown }
    | undefined
  const named = typeof details?.filename === 'string' ? details.filename : ''
  if (step.call.name === 'file_read') {
    const from = typeof details?.startLine === 'number' ? details.startLine : null
    const to = typeof details?.endLine === 'number' ? details.endLine : null
    if (running || !step.result) return t('chat.tool.file-reading')
    if (!named || from === null || to === null) return t('chat.tool.file-read-generic')
    return t('chat.tool.file-read', { name: named, from, to })
  }
  const query = typeof args.query === 'string' ? args.query : ''
  if (running || !step.result || !query) return query ? t('chat.tool.file-searching', { query }) : t('chat.tool.file-searching-generic')
  const count = typeof details?.matches === 'number' ? details.matches : 0
  return named ? t('chat.tool.file-search-in', { name: named, query, count }) : t('chat.tool.file-search', { query, count })
}

/**
 * A sub-agent call that is a step: a message to one, stopping one, or a launch
 * that did not go through (a launch that did is a line of the reply). Named
 * as the result names it, else as the model did.
 */
function subagentStepLabel(t: T, step: ToolActivityStep, args: Record<string, unknown>, running: boolean): string {
  const details = (step.result?.details ?? {}) as { name?: unknown }
  const named = typeof details.name === 'string' ? details.name : typeof args.name === 'string' ? args.name : typeof args.to === 'string' ? args.to : ''
  const name = named || t('subagents.step.unnamed')
  if (step.call.name === 'subagent_message') return running ? t('subagents.step.message-running', { name }) : t('subagents.step.message', { name })
  if (step.call.name === 'subagent_stop') return running ? t('subagents.step.stop-running', { name }) : t('subagents.step.stop', { name })
  return t('subagents.step.failed', { name })
}

/**
 * A writing-block call that is a step rather than a card (MessageItem): a
 * read, a list, or a write that did not go through. A read names the
 * document once its result does; a failure says which kind of change failed,
 * its explanation under it.
 */
function documentStepLabel(t: T, step: ToolActivityStep, args: Record<string, unknown>, running: boolean): string {
  const op = typeof args.op === 'string' ? args.op : ''
  const title = (step.result?.details as { title?: unknown } | undefined)?.title
  const failed = isFailure(step.status)
  switch (op) {
    case 'read':
      if (failed) return t('document.step.read-failed')
      if (typeof title === 'string' && title) return t('document.step.read', { title })
      return running || !step.result ? t('document.step.reading') : t('document.step.read-untitled')
    case 'list':
      return running || !step.result ? t('document.step.listing') : t('document.step.list')
    case 'create':
      return t(failed ? 'document.step.create-failed' : 'document.step.writing')
    case 'update':
      return t(failed ? 'document.step.update-failed' : 'document.step.updating')
    default:
      return t('document.step.generic')
  }
}

export function isFailure(status: ToolStatus) {
  return status !== 'pending' && status !== 'running' && status !== 'succeeded'
}

export function callArguments(call: ToolCallPart): Record<string, unknown> {
  if (call.arguments) return call.arguments
  // Still streaming: the arguments are a JSON fragment that may not parse yet.
  try {
    const parsed: unknown = JSON.parse(call.rawArguments)
    return parsed && typeof parsed === 'object' ? (parsed as Record<string, unknown>) : {}
  } catch {
    return {}
  }
}

/**
 * A search result's excerpt as text to read: the services hand back page text
 * as they scraped it, Markdown links and images, bare addresses, headings and
 * runs of blank lines among it, which read as noise in a list of results.
 */
export function plainSnippet(snippet: string): string {
  return snippet
    .replace(/!\[[^\]]*\]\([^)]*\)/g, '')
    .replace(/\[([^\]]*)\]\([^)]*\)/g, '$1')
    .replace(/https?:\/\/\S+/g, '')
    .replace(/[#*_`>|]+/g, ' ')
    .replace(/\s*(\.{3}|…)\s*$/, '')
    .replace(/\s+/g, ' ')
    .trim()
}

export function searchHits(result?: ToolResultPart): SearchHit[] {
  const results = (result?.details as { results?: unknown } | undefined)?.results
  if (!Array.isArray(results)) return []
  // Only http(s) addresses: these are pages the model read, and each becomes a link.
  return results.filter(
    (hit): hit is SearchHit => typeof hit === 'object' && hit !== null && isHttpUrl((hit as SearchHit).url),
  )
}

/** The activity line's own words and marks, from its steps. */
export interface ActivityHead {
  title: string
  /** The step in flight, if any. */
  active?: ActivityItem & { kind: 'tool' }
  /** How many steps failed, said beside a finished line. */
  failed: number
  /** Every source the searches turned up, once per site, in the order found. */
  sources: SearchHit[]
  steps: (ActivityItem & { kind: 'tool' })[]
  /**
   * Says only how long the model thought (`brief`, when it thought): the
   * searches and their sources are for the lines inside to say.
   */
  thoughtOnly: boolean
}

/**
 * The line while the model works names what it is doing; once it is done, it
 * sums the process up - "thought for 16 seconds, searched 4 times, read 1
 * page", or only how long it thought when it called no tool. The process's
 * own line is `brief`, as ChatGPT's "thought for 20s": once done it says only
 * how long the model thought, when it thought at all. `thinking` is the model
 * at work with no call in flight: streaming its thoughts (`reasoning`), quiet
 * between a round and its next response (`waiting`, when the line keeps naming
 * the step just done), or a failed request being tried again (`retrying`).
 */
export function activityHead(
  t: T,
  items: ActivityItem[],
  thinking: 'reasoning' | 'waiting' | 'retrying' | false,
  brief = false,
): ActivityHead {
  const steps = items.filter((item): item is ActivityItem & { kind: 'tool' } => item.kind === 'tool')
  const active = steps.find((step) => step.status === 'pending' || step.status === 'running')
  const searches = steps.filter((step) => step.call.name === 'web_search').length
  const reads = steps.filter((step) => step.call.name === 'web_fetch')
  // Pages, not reads: reading on in a long page, or looking for something in
  // it, is still the one page.
  const fetches = new Set(reads.map((step) => String(callArguments(step.call).url ?? ''))).size
  const fileLooks = steps.filter((step) => isFileTool(step.call.name)).length
  const others = steps.length - searches - reads.length - fileLooks
  const failed = steps.filter((step) => isFailure(step.status)).length

  const sources: SearchHit[] = []
  const seen = new Set<string>()
  for (const step of steps) {
    for (const hit of searchHits(step.result)) {
      const domain = siteName(hit.url)
      if (seen.has(domain)) continue
      seen.add(domain)
      sources.push(hit)
    }
  }

  const thoughts = items.filter((item): item is ActivityNote & { key: string } => item.kind === 'reasoning')
  const thoughtMs = thoughts.reduce((sum, item) => sum + (item.durationMs ?? 0), 0)
  const thoughtOnly = brief && thoughts.length > 0

  let title: string
  const lastItem = items[items.length - 1]
  if (thinking === 'retrying') {
    title = t('chat.retrying')
  } else if (active) {
    title = stepLabel(t, active, true)
  } else if (thinking === 'waiting' && lastItem?.kind === 'tool') {
    title = stepLabel(t, lastItem, false)
  } else if (thinking) {
    title = t('chat.reasoning.live')
  } else if (thoughtOnly) {
    title = thoughtMs > 0 ? t('chat.reasoning.took', { count: Math.max(1, Math.round(thoughtMs / 1000)) }) : t('chat.reasoning.thought')
  } else {
    const thought =
      thoughts.length === 0
        ? false
        : thoughtMs > 0
          ? t('chat.reasoning.took', { count: Math.max(1, Math.round(thoughtMs / 1000)) })
          : steps.length > 0
            ? t('chat.reasoning.thought')
            : t('chat.reasoning.done')
    const parts = [
      thought,
      searches > 0 && t('chat.tool.searched', { count: searches }),
      fetches > 0 && t('chat.tool.read', { count: fetches }),
      fileLooks > 0 && t('chat.tool.files', { count: fileLooks }),
      others > 0 && t('chat.tool.used', { count: others }),
    ].filter((part): part is string => Boolean(part))
    // English clauses start lower case after the first; the line itself does not.
    title = parts.map((part, i) => (i === 0 ? part.charAt(0).toUpperCase() : part.charAt(0).toLowerCase()) + part.slice(1)).join(t('chat.tool.and'))
  }
  return { title, active, failed, sources, steps, thoughtOnly }
}
