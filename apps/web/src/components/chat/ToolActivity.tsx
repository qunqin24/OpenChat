import { AlarmClock, Brain, ChevronRight, FileSearch, FileText, Globe, NotebookPen, Search, TriangleAlert, Wrench } from 'lucide-react'
import { Collapsible } from 'radix-ui'
import { useLayoutEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { API_BASE_URL } from '@/lib/api'
import { isHttpUrl, siteName } from '@openchat/chat-core/urls'
import {
  activityHead,
  activitySegments,
  isFailure,
  searchHits,
  stepLabel,
  type ActivityItem,
  type ToolActivityStep,
} from '@openchat/chat-core/tool-activity'
import { cn } from '@/lib/utils'
import { ThinkingOrb } from './ThinkingOrb'
import { AnswerText } from './AnswerText'
import { Markdown } from './MarkdownText'
import { SubagentsIcon } from './SubagentAvatar'
import { ThinkingIcon } from './ThinkingIcon'

/** Source chips shown in the collapsed line, before the rest are counted. */
const HEAD_SOURCES = 4
/** Source chips shown under one search in the open list. */
const STEP_SOURCES = 6

/**
 * How a reply was reached - its process (chat-core/reply-process.ts) - folded
 * into one line: what the model is doing while it works, and what it did once
 * it is done - "thought for 16 seconds, searched 4 times" with the sources it
 * found. Opening it lists everything in order on one flat timeline, no fold
 * inside: the thinking, the calls with their sources, and what the model said
 * on its way as it said it - a light timeline rather than a stack of boxes,
 * so the answer below stays the thing the eye lands on.
 *
 * 
 * `thinking` is the model at work with no call in flight: streaming its
 * thoughts (`reasoning`), or quiet between a round and its next response
 * (`waiting`), when the line keeps naming the step just done - it is still
 * reading, as far as the reader can tell - rather than claiming thought - or
 * a failed request being tried again (`retrying`), which the line says.
 */
export function ToolActivity({
  items,
  thinking = false,
  nested = false,
}: {
  items: ActivityItem[]
  thinking?: 'reasoning' | 'waiting' | 'retrying' | false
  /** A run inside the process's fold, between the words said on the way. */
  nested?: boolean
}) {
  const { t } = useTranslation()
  // The process's own line says only how long the model thought; a run inside it says what it did.
  const { title, active, failed, sources, steps, thoughtOnly } = activityHead(t, items, thinking, !nested)
  const segments = activitySegments(items)
  // Open while the model works, so the reader watches it think and search;
  // folded away the moment the answer starts, so the answer is what is left
  // (as ChatGPT has it). Either way the reader may open or close it.
  const working = Boolean(active || thinking)
  const [open, setOpen] = useState(working)
  const [wasWorking, setWasWorking] = useState(working)
  if (working !== wasWorking) {
    setWasWorking(working)
    setOpen(working)
  }
  if (items.length === 0 && !working) return null

  return (
    // A Radix collapsible only for its measured height, so the steps can
    // animate open and closed like the reasoning; the line stays a plain button.
    <Collapsible.Root asChild open={open}>
      <div className={cn('n-activity', open && 'is-open', (active || thinking) && 'is-active')}>
        <button
          type="button"
          className="n-activity-head"
          aria-expanded={open}
          onClick={() => setOpen(!open)}
        >
          <span className="n-activity-icon">
            {active || thinking ? (
              // The same mark as thinking anywhere else - a search or a read in
              // flight too, and between rounds - so the model at work always
              // looks the same.
              <span className="n-activity-orb" role="status" aria-label={active ? t('chat.tool.running') : t('chat.reasoning.live')}><ThinkingOrb /></span>
            ) : steps.length > 0 && !thoughtOnly ? (
              stepIcon(steps[steps.length - 1]!.call.name)
            ) : (
              <ThinkingIcon aria-hidden="true" />
            )}
          </span>
          <span className="n-activity-title">{title}</span>
          {!active && !thinking && !thoughtOnly && failed > 0 && (
            <span className="n-activity-failed">{t('chat.tool.failures', { count: failed })}</span>
          )}
          {sources.length > 0 && !thoughtOnly && (
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
            {segments
              ? segments.map((segment, i) =>
                  segment.kind === 'note' ? (
                    renderItem(segment.item, working)
                  ) : (
                    // Each run of thinking and steps between the words said on
                    // the way is a line of its own, open only while it is the
                    // model's current work.
                    <li key={segment.key} className="n-activity-run">
                      <ToolActivity items={segment.items} thinking={i === segments.length - 1 ? thinking : false} nested />
                    </li>
                  ),
                )
              : items.map((item) => renderItem(item, working))}
          </ol>
        </Collapsible.Content>
      </div>
    </Collapsible.Root>
  )
}

/**
 * A thought on the timeline. While the model works, only its last lines show,
 * in a window that keeps to the newest (as LibreChat peeks at a thought), so a
 * long one does not push the steps after it a screen away; that window does
 * not scroll, since it is moved to the end with every token and a reader
 * scrolling in it was pulled back. Done, a long one is a box of its own height
 * that scrolls, faded at an edge with more beyond it, so the steps after it
 * stay near; at its ends the wheel goes on to the page.
 */
function Thought({ text, streaming, fresh, peek }: { text: string; streaming: boolean; fresh: boolean; peek: boolean }) {
  const box = useRef<HTMLDivElement>(null)
  // Where it is cut, for the fades: above while peeking or scrolled down, below
  // while there is more to scroll to.
  const markCut = () => {
    const el = box.current
    if (!el) return
    const over = el.scrollHeight - el.clientHeight
    el.toggleAttribute('data-cut', over > 1 && el.scrollTop > 1)
    el.toggleAttribute('data-more', !peek && over > 1 && el.scrollTop < over - 1)
  }
  // After every render, as the text grows: kept at its end while it is
  // written; long (a box of its own) when done and taller than it.
  useLayoutEffect(() => {
    const el = box.current
    if (!el) return
    el.toggleAttribute('data-long', !peek && el.scrollHeight > THOUGHT_BOX + THOUGHT_SLACK)
    if (peek) el.scrollTop = el.scrollHeight
    markCut()
  })
  return (
    <div ref={box} className={cn('n-activity-thought', peek && 'is-peek')} onScroll={peek ? undefined : markCut}>
      {/* Markdown, as models write their reasoning in it; plain Markdown, so
          an interface block drafted while thinking shows as the code it is. */}
      <Markdown text={text} streaming={streaming} fresh={fresh && streaming} />
    </div>
  )
}

/** How tall a finished thought's box is (CSS `[data-long]`). */
const THOUGHT_BOX = 240
/** A thought only a line or so taller shows whole: a box to scroll one more line is not worth it. */
const THOUGHT_SLACK = 48

/** One thing on the timeline: a step, a thought, or words the model said on its way. */
function renderItem(item: ActivityItem, working: boolean) {
  if (item.kind === 'tool') return <Step key={item.key} step={item} />
  if (item.kind === 'reasoning')
    return (
      <li key={item.key} className="n-activity-note is-reasoning">
        <Thought text={item.text} streaming={item.streaming ?? false} fresh={item.fresh ?? false} peek={working} />
      </li>
    )
  // Fresh only while it streams: the timeline mounts again each time the
  // reader opens it, and finished words are not faded in again. Drawn as the
  // answer is, interface blocks and all: they were said to the reader.
  return (
    <li key={item.key} className="n-activity-note">
      <AnswerText text={item.text} streaming={item.streaming ?? false} fresh={(item.fresh ?? false) && (item.streaming ?? false)} />
    </li>
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
