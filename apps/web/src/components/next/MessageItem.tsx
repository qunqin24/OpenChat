import type { ChatModel, Message, MessagePart } from '@openchat/api-client'
import { Check, Copy } from 'lucide-react'
import { Collapsible } from 'radix-ui'
import { isValidElement, useState, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import { Streamdown } from 'streamdown'

import { ModelIcon } from '@/components/model-icon'
import { copyClipboard } from '@/lib/clipboard'
import { splitSegments } from '@/lib/genui/segments'
import { turnText, type Turn } from '@/lib/transcript'
import GenUIBlock from './genui/GenUIBlock'
import MessageAttachments from './MessageAttachments'
import { markdownComponents, markdownLinkSafety, markdownPlugins, prepareMarkdown } from './markdown-components'
import ToolActivity, {
  type ActivityItem,
  type ActivityNote,
  type ToolActivityStep,
  type ToolStatus,
} from './ToolActivity'

function CopyButton({ content }: { content: string }) {
  const { t } = useTranslation()
  const [copied, setCopied] = useState(false)

  return (
    <button
      type="button"
      className="n-icon-btn sm"
      aria-label={t('chat.copy')}
      onClick={async () => {
        if (!(await copyClipboard(content))) return
        setCopied(true)
        window.setTimeout(() => setCopied(false), 1600)
      }}
    >
      {copied ? <Check className="h-3.5 w-3.5" /> : <Copy className="h-3.5 w-3.5" />}
    </button>
  )
}

/** User turns render as a right-aligned bubble with no avatar. */
export function UserMessage({ message }: { message: Message }) {
  return (
    <div className="n-msg-user">
      {message.attachments && message.attachments.length > 0 && <MessageAttachments items={message.attachments} />}
      <div className="n-bubble">{message.content}</div>
      <div className="n-actions">
        <CopyButton content={message.content} />
      </div>
    </div>
  )
}

/**
 * An answer's text: Markdown, with any `openchat-ui` blocks in it drawn as
 * components — including one still being written at the end of a stream.
 */
/** An answer's text: Markdown with any interface blocks drawn where they sit. */
export function AnswerText({ text, streaming }: { text: string; streaming: boolean }) {
  const segments = splitSegments(text, streaming)
  return segments.map((segment, i) =>
    segment.kind === 'ui' ? (
      <GenUIBlock key={i} source={segment.source} complete={segment.complete} />
    ) : (
      <Markdown key={i} text={segment.text} streaming={streaming && i === segments.length - 1} />
    ),
  )
}

function Markdown({ text, streaming }: { text: string; streaming: boolean }) {
  return (
    <div className="n-markdown">
      {/* The local typography layer styles the actual Streamdown output;
          markdown-body keeps the ported table selectors working. */}
      <div className="markdown-body">
        <Streamdown className="n-prose" parseIncompleteMarkdown={streaming} components={markdownComponents} plugins={markdownPlugins} linkSafety={markdownLinkSafety}>
          {prepareMarkdown(text)}
        </Streamdown>
      </div>
    </div>
  )
}

/**
 * The model's reasoning. Open while it is being written, so the reader can
 * watch the model think; folded away the moment it is done, into how long it
 * took, because the answer is what comes next. Either way the reader can open
 * or close it. Withheld reasoning — the provider sends only an encrypted blob —
 * has nothing to show and is left out.
 */
function Reasoning({ text, streaming, durationMs }: { text: string; streaming: boolean; durationMs?: number }) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(streaming)
  // Follows the stream when it starts and stops; in between, the reader's.
  const [wasStreaming, setWasStreaming] = useState(streaming)
  if (streaming !== wasStreaming) {
    setWasStreaming(streaming)
    setOpen(streaming)
  }
  if (!text && !streaming) return null

  const seconds = durationMs === undefined ? null : Math.max(1, Math.round(durationMs / 1000))
  return (
    // A Radix collapsible rather than <details>, so the body can animate
    // open and closed: <details> drops its content the instant it closes.
    <Collapsible.Root className="n-reasoning" open={open && Boolean(text)} onOpenChange={setOpen}>
      <Collapsible.Trigger className="n-reasoning-toggle">
        {streaming ? (
          <span className="n-reasoning-live" data-motion="essential">
            {t('chat.reasoning.live')}
          </span>
        ) : seconds === null ? (
          t('chat.reasoning.done')
        ) : (
          t('chat.reasoning.took', { count: seconds })
        )}
      </Collapsible.Trigger>
      <Collapsible.Content className="n-reasoning-content">
        {/* Markdown, as models write their reasoning in it; plain Markdown and
            not AnswerText, so an interface block drafted while thinking shows
            as the code it is rather than as a component. */}
        <div className="n-reasoning-body">
          <Markdown text={text} streaming={streaming} />
        </div>
      </Collapsible.Content>
    </Collapsible.Root>
  )
}

function Notice({ tone, children }: { tone: 'error' | 'quiet'; children: ReactNode }) {
  return (
    <p className={tone === 'error' ? 'n-notice is-error' : 'n-notice'} role={tone === 'error' ? 'alert' : undefined}>
      {children}
    </p>
  )
}

const ERROR_CODES = [
  'no_model',
  'model_error',
  'model_auth',
  'model_rate_limited',
  'context_too_long',
  'internal',
] as const

function errorText(t: ReturnType<typeof useTranslation>['t'], code?: string | null) {
  const known = ERROR_CODES.find((c) => c === code)
  return known ? t(`chat.error.${known}`) : t('chat.error.model_error')
}

interface AssistantTurnProps {
  turn: Extract<Turn, { kind: 'assistant' }>
  /** The catalogue's entry for the model, for its name and mark. */
  model?: ChatModel
  modelId: string
}

/**
 * One reply: every response and tool step of a run, in the order they
 * happened, under one model header.
 */
export function AssistantTurn({ turn, model, modelId }: AssistantTurnProps) {
  const { t } = useTranslation()
  const live = turn.live
  const running = live?.run.status === 'running'

  const toolStatus = (callId: string): ToolStatus => {
    const result = turn.results[callId]
    if (result) return result.status
    const slot = live?.tools[callId]
    if (slot === 'running') return 'running'
    return running ? 'pending' : 'interrupted'
  }

  // Tool calls are collected rather than drawn, and text and reasoning carry
  // what they would fold into: the stretch from the first tool call to the
  // last becomes one activity line (see `blocks` below).
  const renderParts = (parts: MessagePart[], keyPrefix: string, streaming: boolean): Block[] =>
    parts.map((part, index) => {
      const key = `${keyPrefix}:${index}`
      const isLast = index === parts.length - 1
      switch (part.type) {
        case 'reasoning':
          return {
            key,
            node: <Reasoning key={key} text={part.text} durationMs={part.durationMs} streaming={streaming && isLast} />,
            fold: { kind: 'reasoning', text: part.text },
          }
        case 'text':
          return part.text.trim()
            ? {
                key,
                node: <AnswerText key={key} text={part.text} streaming={streaming && isLast} />,
                fold: { kind: 'note', text: part.text },
              }
            : null
        case 'tool_call':
          return { key, step: { call: part, result: turn.results[part.id], status: toolStatus(part.id) } }
        default:
          return null
      }
    })

  const entries: Block[] = []
  for (const response of turn.responses) {
    entries.push(...renderParts(response.parts, response.id, false))
    if (response.stopReason === 'error') {
      entries.push(
        <Notice key={`${response.id}:error`} tone="error">
          {errorText(t, response.errorCode)}
        </Notice>,
      )
    } else if (response.stopReason === 'cancelled') {
      entries.push(
        <Notice key={`${response.id}:stopped`} tone="quiet">
          {t('chat.stopped')}
        </Notice>,
      )
    } else if (response.stopReason === 'length') {
      entries.push(
        <Notice key={`${response.id}:length`} tone="quiet">
          {t('chat.truncated')}
        </Notice>,
      )
    }
  }
  if (turn.partial) entries.push(...renderParts(turn.partial, 'partial', running))
  if (running && live?.retry) {
    entries.push(
      <Notice key="retry" tone="quiet">
        {t('chat.retrying')}
      </Notice>,
    )
  }

  // One activity line per reply. Everything from the first tool call to the
  // last — the calls, and whatever the model said or thought between rounds,
  // which is narration of the search rather than the answer — folds into it,
  // in order. What comes before stays as written; what comes after is the
  // answer. A notice inside that stretch is shown just after the line.
  const first = entries.findIndex(isToolBlock)
  const last = entries.length - 1 - [...entries].reverse().findIndex(isToolBlock)
  const blocks: ReactNode[] = []
  const after: ReactNode[] = []
  const items: ActivityItem[] = []
  entries.forEach((entry, i) => {
    const inside = first >= 0 && i >= first && i <= last
    if (inside && isToolBlock(entry)) {
      items.push({ kind: 'tool', key: entry.key, ...entry.step })
    } else if (inside && isFoldBlock(entry)) {
      if (entry.fold.text.trim()) items.push({ ...entry.fold, key: entry.key })
    } else if (inside) {
      if (entry !== null) after.push(entry as ReactNode)
    } else {
      const node = isFoldBlock(entry) ? entry.node : entry
      if (node !== null && !isToolBlock(node)) blocks.push(node)
    }
    if (i === last) blocks.push(<ToolActivity key="activity" items={items} />, ...after)
  })

  // Working, with nothing on screen moving: before the first token, and
  // between a tool round and the next response.
  const toolRunning = Object.values(live?.tools ?? {}).includes('running')
  const waiting = running && !toolRunning && (!turn.partial || turn.partial.length === 0)
  const text = turnText(turn)
  const name = model?.name ?? modelId


  return (
    <div className="n-msg-assistant">
      {/* Just the mark, filling its slot but never cropped: brand marks are
          not circles, and a round crop cut the points off Claude's. */}
      <span className="n-msg-avatar n-msg-mark" aria-hidden="true">
        <ModelIcon
          subject="model"
          className="size-8"
          providerId={model?.providerId || undefined}
          family={model?.family || undefined}
          name={name}
        />
      </span>
      <div className="n-msg-head">{name}</div>
      <div className="n-msg-body">{blocks}</div>

      {waiting && <span className="n-caret" />}

      {!running && text && (
        <div className="n-actions">
          <CopyButton content={text} />
        </div>
      )}
    </div>
  )
}

/** A tool call waiting to be folded into an activity line. */
type ToolBlock = { key: string; step: ToolActivityStep }
/** Text or reasoning: drawn as `node`, or folded into the activity as `fold`. */
type FoldBlock = { key: string; node: ReactNode; fold: ActivityNote }
type Block = ReactNode | ToolBlock | FoldBlock

function isToolBlock(block: Block): block is ToolBlock {
  return typeof block === 'object' && block !== null && 'step' in block && !isValidElement(block)
}

function isFoldBlock(block: Block): block is FoldBlock {
  return typeof block === 'object' && block !== null && 'fold' in block && !isValidElement(block)
}
