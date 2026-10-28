import type { TFunction } from 'i18next'
import type { ToolCallPart, ToolResultPart } from '@openchat/api-client'
import { AlarmClock, Brain, ChevronRight, FileSearch, FileText, Globe, NotebookPen, Search, TriangleAlert, Wrench } from 'lucide-react'
import { Collapsible } from 'radix-ui'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { keepHeaderInPlace, useStuck } from '@/hooks/use-stuck'
import { API_BASE_URL } from '@/lib/api'
import { isHttpUrl, siteName } from '@/lib/urls'
import { cn } from '@/lib/utils'
import { ThinkingOrb } from './ThinkingOrb'
import { PlainReveal } from './MarkdownText'
import { SubagentsIcon } from './SubagentAvatar'

export type ToolStatus = 'pending' | 'running' | ToolResultPart['status']

export interface ToolActivityStep {
  call: ToolCallPart
  result?: ToolResultPart
  status: ToolStatus
}

/** What the model said or thought between tool rounds. */
export interface ActivityNote {
  kind: 'note' | 'reasoning'
  text: string
  /** Still being written: revealed as an answer's text is (PlainReveal). */
  streaming?: boolean
}

/** One entry of the activity, in the order it happened. */
export type ActivityItem = ({ kind: 'tool' } & ToolActivityStep | ActivityNote) & { key: string }

interface SearchHit {
  title: string
  url: string
}

/** Source chips shown in the collapsed line, before the rest are counted. */
const HEAD_SOURCES = 4
/** Source chips shown under one search in the open list. */
const STEP_SOURCES = 6

/**
 * How a reply was reached, folded into one line: what the model is doing while
 * it works, and what it did once it is done — "searched 4 times, read 1 page"
 * with the sources it found. Opening it lists every step in order — the calls
 * with their sources, and what the model said or thought between them — as a
 * light timeline rather than a stack of boxes, so the answer below stays the
 * thing the eye lands on.
 */
/**
 * `thinking` is the model at work with no call in flight: streaming its
 * thoughts (`reasoning`), or quiet between a round and its next response
 * (`waiting`), when the line keeps naming the step just done - it is still
 * reading, as far as the reader can tell - rather than claiming thought.
 */
export function ToolActivity({ items, thinking = false }: { items: ActivityItem[]; thinking?: 'reasoning' | 'waiting' | false }) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  const [head, stuck] = useStuck<HTMLButtonElement>(open)
  const steps = items.filter((item): item is ActivityItem & { kind: 'tool' } => item.kind === 'tool')

  const active = steps.find((step) => step.status === 'pending' || step.status === 'running')
  const searches = steps.filter((step) => step.call.name === 'web_search').length
  const fetches = steps.filter((step) => step.call.name === 'web_fetch').length
  const fileLooks = steps.filter((step) => isFileTool(step.call.name)).length
  const others = steps.length - searches - fetches - fileLooks
  const failed = steps.filter((step) => isFailure(step.status)).length

  // Every source the searches turned up, once each, in the order found.
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

  let title: string
  const lastItem = items[items.length - 1]
  if (active) {
    title = stepLabel(t, active, true)
  } else if (thinking === 'waiting' && lastItem?.kind === 'tool') {
    title = stepLabel(t, lastItem, false)
  } else if (thinking) {
    title = t('chat.reasoning.live')
  } else {
    const parts = [
      searches > 0 && t('chat.tool.searched', { count: searches }),
      fetches > 0 && t('chat.tool.read', { count: fetches }),
      fileLooks > 0 && t('chat.tool.files', { count: fileLooks }),
      others > 0 && t('chat.tool.used', { count: others }),
    ].filter(Boolean)
    const joined = parts.join(t('chat.tool.and'))
    // English starts lower case after the first clause; the line itself does not.
    title = joined.charAt(0).toUpperCase() + joined.slice(1)
  }

  return (
    // A Radix collapsible only for its measured height, so the steps can
    // animate open and closed like the reasoning; the line stays a plain button.
    <Collapsible.Root asChild open={open}>
      <div className={cn('n-activity', open && 'is-open', (active || thinking) && 'is-active')}>
        <button
          ref={head}
          type="button"
          className="n-activity-head"
          data-stuck={stuck || undefined}
          aria-expanded={open}
          onClick={() => {
            // Closed from where it is pinned: stay at the line, not below the answer.
            if (open) keepHeaderInPlace(head.current)
            setOpen(!open)
          }}
        >
          <span className="n-activity-icon">
            {active || thinking ? (
              // The same mark as thinking anywhere else - a search or a read in
              // flight too, and between rounds - so the model at work always
              // looks the same.
              <span className="n-activity-orb" role="status" aria-label={active ? t('chat.tool.running') : t('chat.reasoning.live')}><ThinkingOrb /></span>
            ) : (
              stepIcon(steps[steps.length - 1]!.call.name)
            )}
          </span>
          <span className="n-activity-title">{title}</span>
          {!active && !thinking && failed > 0 && (
            <span className="n-activity-failed">{t('chat.tool.failures', { count: failed })}</span>
          )}
          {sources.length > 0 && (
            <span className="n-activity-sources" aria-hidden="true">
              {sources.slice(0, HEAD_SOURCES).map((hit) => (
                <SourceMark key={hit.url} url={hit.url} />
              ))}
              {sources.length > HEAD_SOURCES && (
                <span className="n-activity-more">+{sources.length - HEAD_SOURCES}</span>
              )}
            </span>
          )}
          <ChevronRight className="n-activity-chevron" aria-hidden="true" />
        </button>

        <Collapsible.Content className="n-activity-content">
          <ol className="n-activity-steps">
            {items.map((item) =>
              item.kind === 'tool' ? (
                <Step key={item.key} step={item} />
              ) : (
                <li key={item.key} className={cn('n-activity-note', item.kind === 'reasoning' && 'is-reasoning')}>
                  <PlainReveal text={item.text} streaming={item.streaming ?? false} />
                </li>
              ),
            )}
          </ol>
        </Collapsible.Content>
      </div>
    </Collapsible.Root>
  )
}

function Step({ step }: { step: ToolActivityStep }) {
  const { t } = useTranslation()
  const hits = searchHits(step.result)
  const fetched = step.call.name === 'web_fetch' && step.result?.status === 'succeeded'
    ? (step.result.details as { url?: string; title?: string } | undefined)
    : undefined

  return (
    <li className={cn('n-activity-step', isFailure(step.status) && 'is-failed')}>
      <span className="n-activity-step-icon">
        {isFailure(step.status) ? <TriangleAlert aria-hidden="true" /> : stepIcon(step.call.name)}
      </span>
      <div className="n-activity-step-body">
        <div className="n-activity-step-label">{stepLabel(t, step, false)}</div>
        {hits.length > 0 && (
          <div className="n-activity-hits">
            {hits.slice(0, STEP_SOURCES).map((hit) => (
              <a key={hit.url} className="n-activity-hit" href={hit.url} target="_blank" rel="noreferrer" title={hit.title}>
                <SourceMark url={hit.url} />
                <span>{siteName(hit.url)}</span>
              </a>
            ))}
            {hits.length > STEP_SOURCES && <span className="n-activity-more">+{hits.length - STEP_SOURCES}</span>}
          </div>
        )}
        {fetched?.url &&
          (isHttpUrl(fetched.url) ? (
            <a className="n-activity-link" href={fetched.url} target="_blank" rel="noreferrer">
              {fetched.title || fetched.url}
            </a>
          ) : (
            // Not a web address: shown, never linked.
            <span className="n-activity-link">{fetched.title || fetched.url}</span>
          ))}
        {/* A failure's explanation is the text the model read; it is written
            for the model, in English, but it is the only account of what
            went wrong. */}
        {step.result && isFailure(step.status) && <p className="n-activity-error">{step.result.content}</p>}
      </div>
    </li>
  )
}

/**
 * A source's mark: the site's icon, served through our own server (which
 * fetches and caches it, so the reader's browser asks no third party about
 * what the answer drew on), with the site's first letter on a neutral disc
 * until it arrives and wherever there is none.
 */
export function SourceMark({ url }: { url: string }) {
  const domain = siteName(url)
  const [failed, setFailed] = useState(false)
  const letter = domain.charAt(0).toUpperCase() || '·'
  return (
    <span className="n-activity-mark">
      {letter}
      {!failed && domain && (
        <img
          src={`${API_BASE_URL}/favicons/${encodeURIComponent(domain)}`}
          alt=""
          loading="lazy"
          decoding="async"
          onError={() => setFailed(true)}
          // The server answers a site without an icon with a 1×1 image (an
          // error status would be logged in the console for every source).
          onLoad={(e) => e.currentTarget.naturalWidth <= 1 && setFailed(true)}
        />
      )}
    </span>
  )
}

function stepIcon(name: string) {
  if (name === 'web_search') return <Search aria-hidden="true" />
  if (name === 'web_fetch') return <Globe aria-hidden="true" />
  if (name === 'file_read') return <FileText aria-hidden="true" />
  if (name === 'file_search') return <FileSearch aria-hidden="true" />
  if (name === 'memory') return <Brain aria-hidden="true" />
  if (name === 'scheduled_tasks') return <AlarmClock aria-hidden="true" />
  if (name === 'document') return <NotebookPen aria-hidden="true" />
  if (name.startsWith('subagent')) return <SubagentsIcon />
  return <Wrench aria-hidden="true" />
}

type T = TFunction<'common'>

function stepLabel(t: T, step: ToolActivityStep, running: boolean): string {
  const args = callArguments(step.call)
  if (step.call.name === 'web_search') {
    const query = typeof args.query === 'string' ? args.query : ''
    if (!query) return t('chat.tool.search-pending')
    return running ? t('chat.tool.searching', { query }) : query
  }
  if (step.call.name === 'web_fetch') {
    const url = typeof args.url === 'string' ? args.url : ''
    if (!url) return t('chat.tool.fetch-pending')
    return running ? t('chat.tool.reading', { host: siteName(url) }) : t('chat.tool.fetch', { host: siteName(url) })
  }
  if (isFileTool(step.call.name)) return fileStepLabel(t, step, args, running)
  if (step.call.name === 'memory') return t('chat.memory.step')
  if (step.call.name === 'scheduled_tasks') return running ? t('tasks.step-running') : t('tasks.step')
  if (step.call.name === 'document') return documentStepLabel(t, step, args, running)
  if (step.call.name.startsWith('subagent')) return subagentStepLabel(t, step, args, running)
  return t('chat.tool.generic', { name: step.call.name })
}

/** file_read and file_search: the tools that read a long text file the user attached (agent/files.go). */
function isFileTool(name: string) {
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

function isFailure(status: ToolStatus) {
  return status !== 'pending' && status !== 'running' && status !== 'succeeded'
}

function callArguments(call: ToolCallPart): Record<string, unknown> {
  if (call.arguments) return call.arguments
  // Still streaming: the arguments are a JSON fragment that may not parse yet.
  try {
    const parsed: unknown = JSON.parse(call.rawArguments)
    return parsed && typeof parsed === 'object' ? (parsed as Record<string, unknown>) : {}
  } catch {
    return {}
  }
}

function searchHits(result?: ToolResultPart): SearchHit[] {
  const results = (result?.details as { results?: unknown } | undefined)?.results
  if (!Array.isArray(results)) return []
  // Only http(s) addresses: these are pages the model read, and each becomes a link.
  return results.filter(
    (hit): hit is SearchHit => typeof hit === 'object' && hit !== null && isHttpUrl((hit as SearchHit).url),
  )
}

export default ToolActivity
