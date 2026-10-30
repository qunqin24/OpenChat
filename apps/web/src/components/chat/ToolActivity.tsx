import { AlarmClock, Brain, ChevronRight, FileSearch, FileText, Globe, NotebookPen, Search, TriangleAlert, Wrench } from 'lucide-react'
import { Collapsible } from 'radix-ui'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { keepHeaderInPlace, useStuck } from '@/hooks/use-stuck'
import { API_BASE_URL } from '@/lib/api'
import { isHttpUrl, siteName } from '@openchat/chat-core/urls'
import {
  activityHead,
  isFailure,
  searchHits,
  stepLabel,
  type ActivityItem,
  type ToolActivityStep,
} from '@openchat/chat-core/tool-activity'
import { cn } from '@/lib/utils'
import { ThinkingOrb } from './ThinkingOrb'
import { PlainReveal } from './MarkdownText'
import { SubagentsIcon } from './SubagentAvatar'

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
 * reading, as far as the reader can tell - rather than claiming thought - or
 * a failed request being tried again (`retrying`), which the line says.
 */
export function ToolActivity({
  items,
  thinking = false,
}: {
  items: ActivityItem[]
  thinking?: 'reasoning' | 'waiting' | 'retrying' | false
}) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  const [head, stuck] = useStuck<HTMLButtonElement>(open)
  const { title, active, failed, sources, steps } = activityHead(t, items, thinking)

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

export default ToolActivity
