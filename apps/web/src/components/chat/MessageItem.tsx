import type { TFunction } from 'i18next'
import type { ChatModel, Message, MessagePart } from '@openchat/api-client'
import { MessageSquareQuote, NotebookPen } from 'lucide-react'
import { Collapsible } from 'radix-ui'
import { isValidElement, useLayoutEffect, useRef, useState, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'

import { ModelIcon } from '@/components/model-icon'
import { keepHeaderInPlace, useStuck } from '@/hooks/use-stuck'
import { answersOf, ASK_USER, parseQuestions } from '@/lib/ask-user'
import { copyClipboard } from '@/lib/clipboard'
import { splitSegments } from '@/lib/genui/segments'
import { turnText, type Turn } from '@/lib/transcript'
import { CopiedIcon, CopyIcon, EditIcon, RegenerateIcon } from './ActionIcons'
import AskUserCard, { PreparingLine } from './AskUserCard'
import { MEMORY_TOOL, memoryChangeOf } from '@/lib/memory-tool'
import { TASK_TOOL, taskChangeOf } from '@/lib/task-tool'
import { DOCUMENT_TOOL, documentChangeOf } from '@/lib/document-tool'
import { launchesOf, SUBAGENT_TOOL } from '@/lib/subagents'
import { SubagentLaunchLine } from './SubagentLines'
import { parseQuotes, quoteMessage } from '@/lib/document-selection'
import { highlightPassage } from '@/lib/highlight-passage'
import { cn } from '@/lib/utils'
import GenUIBlock from './genui/GenUIBlock'
import MemoryChip from './MemoryChip'
import TaskChip from './TaskChip'
import DocumentCard from './DocumentCard'
import MessageAttachments from './MessageAttachments'
import { Markdown } from './MarkdownText'
import CompactionDivider from './CompactionDivider'
import { CompactingLine, ThinkingLine, ThinkingOrb } from './ThinkingOrb'
import type { Refusal } from '@/lib/moderation-refusal'
import RefusalCard from './RefusalCard'
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
      {copied ? <CopiedIcon /> : <CopyIcon />}
    </button>
  )
}

/**
 * User turns render as a right-aligned bubble with no avatar. Any of them can
 * be edited in place (`onEdit`): the bubble becomes a field, and sending it
 * asks the question again in the new words, replacing it and the turns after it.
 */
export function UserMessage({
  message,
  onEdit,
  restoredDraft,
  onDraftDone,
  time,
}: {
  message: Message
  onEdit?: (content: string) => void
  /**
   * When it was sent, shown above it because it came after a pause
   * (lib/time-gap.ts): "今天 10:44".
   */
  time?: string
  /**
   * The wording of an edit the server refused (content moderation, a limit):
   * the field opens with it again, so what was typed is not lost. The turn
   * itself was hidden while the edit was out, so this is not kept in the field's
   * own state.
   */
  restoredDraft?: string
  /** The field was sent or put away: the restored wording is no longer wanted. */
  onDraftDone?: () => void
}) {
  const { t } = useTranslation()
  const [typed, setTyped] = useState<string | null>(null)
  // A request about a quoted passage of a writing block: the passage is a card
  // above the words, and editing changes the words only.
  const quoted = parseQuotes(message.content)
  const words = quoted ? quoted.request : message.content
  const draft = typed ?? restoredDraft ?? null
  const files = (message.attachments?.length || message.removedAttachments?.length || message.references?.length) ? (
    <MessageAttachments
      conversationId={message.conversationId}
      items={message.attachments ?? []}
      removed={message.removedAttachments}
      references={message.references}
    />
  ) : null
  const closeField = () => {
    setTyped(null)
    onDraftDone?.()
  }

  if (draft !== null) {
    return (
      <div className="n-msg-user is-editing">
        {files}
        <EditField
          value={draft}
          onChange={setTyped}
          onCancel={closeField}
          onSubmit={
            onEdit &&
            ((content) => {
              closeField()
              onEdit(quoted ? quoteMessage(quoted.quotes, content) : content)
            })
          }
        />
      </div>
    )
  }

  return (
    <div className="n-msg-user">
      {time && <div className="n-msg-time">{time}</div>}
      {files}
      {quoted && (
        <div className="n-quote-cards">
          {quoted.quotes.map((quote, i) => (
            <QuoteCard
              key={i}
              title={quote.title}
              passage={quote.passage}
              fromAnswer={!quote.documentId}
              n={quoted.quotes.length > 1 ? i + 1 : undefined}
            />
          ))}
        </div>
      )}
      <div className="n-bubble">{words}</div>
      <div className="n-actions">
        <CopyButton content={words} />
        {onEdit && (
          <button type="button" className="n-icon-btn sm" aria-label={t('chat.edit')} onClick={() => setTyped(words)}>
            <EditIcon />
          </button>
        )}
      </div>
    </div>
  )
}

/** The passage a request is about - of a writing block or an answer - above its words. */
function QuoteCard({ title, passage, fromAnswer, n }: { title: string; passage: string; fromAnswer: boolean; n?: number }) {
  const { t } = useTranslation()
  // A passage of an answer is shown where it is at a click (highlightPassage).
  const [shown, setShown] = useState(false)
  if (fromAnswer) {
    return (
      <button
        type="button"
        className={cn('n-quote-card is-sent is-showable', shown && 'is-shown')}
        aria-pressed={shown}
        aria-label={t('chat.quote-show')}
        onClick={() => {
          const next = !shown && highlightPassage(passage)
          if (!next) highlightPassage(null)
          setShown(next)
        }}
      >
        <p className="n-quote-text" dir="auto">{passage}</p>
        <span className="n-quote-label">
          <MessageSquareQuote aria-hidden="true" />
          {n ? t('chat.quote-label-n', { n }) : t('chat.quote-label')}
        </span>
      </button>
    )
  }
  return (
    <div className="n-quote-card is-sent">
      <p className="n-quote-text" dir="auto">{passage}</p>
      <span className="n-quote-label">
        {fromAnswer ? <MessageSquareQuote aria-hidden="true" /> : <NotebookPen aria-hidden="true" />}
        {fromAnswer ? (n ? t('chat.quote-label-n', { n }) : t('chat.quote-label')) : title || t('document.quote-label')}
      </span>
    </div>
  )
}

/**
 * The question being reworded: a field the size of what is in it, Enter to
 * send and Escape to put it back, as in the composer. Unchanged wording sends
 * too — it is asked again, with whatever model and settings the composer has
 * now. Held back only when empty, or while the conversation
 * is busy (no `onSubmit`).
 */
function EditField({
  value,
  onChange,
  onCancel,
  onSubmit,
}: {
  value: string
  onChange: (value: string) => void
  onCancel: () => void
  onSubmit?: (content: string) => void
}) {
  const { t } = useTranslation()
  const field = useRef<HTMLTextAreaElement>(null)
  const content = value.trim()
  const ready = Boolean(onSubmit) && content !== ''

  // Grows with the text, up to the cap in CSS, after which it scrolls.
  useLayoutEffect(() => {
    const el = field.current
    if (!el) return
    el.style.height = 'auto'
    el.style.height = `${el.scrollHeight}px`
    // A scrollbar only once the text is past the cap: at its own height a
    // sub-pixel rounding was enough to show one on a single line.
    el.style.overflowY = el.scrollHeight > el.clientHeight + 1 ? 'auto' : 'hidden'
  }, [value])

  const submit = () => {
    if (ready) onSubmit?.(content)
  }

  return (
    <div className="n-edit">
      <textarea
        ref={field}
        className="n-edit-field"
        value={value}
        rows={1}
        aria-label={t('chat.edit')}
        autoFocus
        onFocus={(e) => e.currentTarget.setSelectionRange(e.currentTarget.value.length, e.currentTarget.value.length)}
        onChange={(e) => onChange(e.target.value)}
        onKeyDown={(e) => {
          if (e.key === 'Escape') {
            e.preventDefault()
            onCancel()
          } else if (e.key === 'Enter' && !e.shiftKey && !e.nativeEvent.isComposing) {
            e.preventDefault()
            submit()
          }
        }}
      />
      <div className="n-edit-actions">
        <button type="button" className="n-edit-btn" onClick={onCancel}>
          {t('chat.edit-cancel')}
        </button>
        <button type="button" className="n-edit-btn is-primary" disabled={!ready} onClick={submit}>
          {t('chat.edit-send')}
        </button>
      </div>
    </div>
  )
}

/**
 * An answer's text: Markdown, with any `openchat-ui` blocks in it drawn as
 * components — including one still being written at the end of a stream.
 */
/**
 * An answer's text: Markdown with any interface blocks drawn where they sit.
 * `fresh`: it began while the reader watched (`Markdown`).
 */
export function AnswerText({ text, streaming, fresh = false }: { text: string; streaming: boolean; fresh?: boolean }) {
  const segments = splitSegments(text, streaming)
  return segments.map((segment, i) =>
    segment.kind === 'ui' ? (
      <GenUIBlock key={i} source={segment.source} complete={segment.complete} />
    ) : (
      <Markdown key={i} text={segment.text} streaming={streaming && i === segments.length - 1} fresh={fresh} />
    ),
  )
}

/**
 * The model's reasoning. Open while it is being written, so the reader can
 * watch the model think; folded away the moment it is done, into how long it
 * took, because the answer is what comes next. Either way the reader can open
 * or close it. Withheld reasoning — the provider sends only an encrypted blob —
 * has nothing to show and is left out.
 */
function Reasoning({ text, streaming, durationMs, fresh }: { text: string; streaming: boolean; durationMs?: number; fresh: boolean }) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(streaming)
  // Follows the stream when it starts and stops; in between, the reader's.
  const [wasStreaming, setWasStreaming] = useState(streaming)
  if (streaming !== wasStreaming) {
    setWasStreaming(streaming)
    setOpen(streaming)
  }
  const [toggle, stuck] = useStuck<HTMLButtonElement>(open && Boolean(text))
  if (!text && !streaming) return null

  const seconds = durationMs === undefined ? null : Math.max(1, Math.round(durationMs / 1000))
  return (
    // A Radix collapsible rather than <details>, so the body can animate
    // open and closed: <details> drops its content the instant it closes.
    <Collapsible.Root
      className="n-reasoning"
      open={open && Boolean(text)}
      onOpenChange={(next) => {
        // Closed from where it is pinned: stay at the toggle, not below the answer.
        if (!next) keepHeaderInPlace(toggle.current)
        setOpen(next)
      }}
    >
      <Collapsible.Trigger ref={toggle} className="n-reasoning-toggle" data-stuck={stuck || undefined}>
        {streaming ? (
          <>
            <ThinkingOrb />
            <span className="n-reasoning-live" data-motion="essential">
              {t('chat.reasoning.live')}
            </span>
          </>
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
          {/* Fresh only while it streams: the body mounts again each time
              the reader opens it, and a finished one is not typed again. */}
          <Markdown text={text} streaming={streaming} fresh={fresh && streaming} />
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

/**
 * The failed run's id, shortened, to quote when reporting it: every line the
 * server logged about the run carries the full id, which this is the start of.
 * A click copies it.
 */
function ErrorId({ id }: { id: string }) {
  const { t } = useTranslation()
  const [copied, setCopied] = useState(false)
  return (
    <button
      type="button"
      className="n-error-id"
      title={t('chat.error-id-copy')}
      onClick={async () => {
        if (await copyClipboard(id)) {
          setCopied(true)
          window.setTimeout(() => setCopied(false), 1500)
        }
      }}
    >
      {copied ? t('chat.error-id-copied') : t('chat.error-id', { id })}
    </button>
  )
}

const ERROR_CODES = [
  'no_model',
  'model_error',
  'model_auth',
  'model_rate_limited',
  'context_too_long',
  'model_capacity_reached',
  'internal',
  'tool_markup',
  'connection_lost',
] as const

function errorText(t: TFunction<'common'>, code?: string | null) {
  const known = ERROR_CODES.find((c) => c === code)
  return known ? t(`chat.error.${known}`) : t('chat.error.model_error')
}

interface AssistantTurnProps {
  turn: Extract<Turn, { kind: 'assistant' }>
  /** The catalogue's entry for the model, for its name and mark. */
  model?: ChatModel
  modelId: string
  /** The last answer only: answer again. Absent while that cannot be done. */
  onRegenerate?: () => void
  /** The call id of the model's question this reply is waiting on, if it is. */
  askingCallId?: string | null
  /** Brings the waiting question's panel back over the composer. */
  onOpenQuestion?: () => void
  /** What the divider drawn inside this reply (`turn.inside`) can do. */
  divider?: { onUndo?: () => void; undoing?: boolean }
}

/**
 * One reply: every response and tool step of a run, in the order they
 * happened, under one model header.
 */
export function AssistantTurn({
  turn,
  model,
  modelId,
  onRegenerate,
  askingCallId,
  onOpenQuestion,
  divider,
}: AssistantTurnProps) {
  const { t } = useTranslation()
  const live = turn.live
  const running = live?.run.status === 'running'
  const conversationId = live?.run.conversationId ?? turn.responses[0]?.conversationId ?? turn.reports[0]?.message.conversationId

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
  // `fresh(index)`: that part began while the reader watched (`LiveRun.fresh`).
  const renderParts = (parts: MessagePart[], keyPrefix: string, streaming: boolean, fresh: (index: number) => boolean): Block[] =>
    parts.map((part, index) => {
      const key = `${keyPrefix}:${index}`
      const isLast = index === parts.length - 1
      switch (part.type) {
        case 'reasoning':
          return {
            key,
            node: <Reasoning key={key} text={part.text} durationMs={part.durationMs} streaming={streaming && isLast} fresh={fresh(index)} />,
            fold: { kind: 'reasoning', text: part.text, streaming: streaming && isLast },
          }
        case 'text':
          return part.text.trim()
            ? {
                key,
                node: <AnswerText key={key} text={part.text} streaming={streaming && isLast} fresh={fresh(index)} />,
                fold: { kind: 'note', text: part.text, streaming: streaming && isLast },
              }
            : null
        case 'tool_call': {
          if (part.name === ASK_USER) {
            // The model's question is a card in the reply, not a step of the
            // activity line.
            const questions = parseQuestions(part.arguments)
            const result = turn.results[part.id]
            // One the server refused (a second call in a reply, or arguments
            // it could not put) never reached the person, and the model has
            // been told what to do instead: it is not a failure of theirs to
            // look at, so it is left out rather than drawn as an error step.
            if (result?.status === 'failed') return null
            if (!questions) {
              // Still being written: nothing to ask yet, but not nothing to say.
              if (streaming) return <PreparingLine key={key} />
            } else {
              const answers = answersOf(result)
              // Unanswered is only "skipped" once the reply is over and the
              // question is not the one waiting: while the run goes on it may
              // still be the question the reply stops on.
              const state = answers ? 'answered' : !result && (part.id === askingCallId || running) ? 'pending' : 'skipped'
              return (
                <AskUserCard
                  key={key}
                  questions={questions}
                  state={state}
                  answers={answers}
                  onOpen={state === 'pending' ? onOpenQuestion : undefined}
                />
              )
            }
          }
          if (part.name === MEMORY_TOOL) {
            // A note the model kept is one quiet line in the reply, with its
            // undo, not a step of the activity line. One that failed (the list
            // is full, say) is a failed step like any other.
            const change = memoryChangeOf(turn.results[part.id])
            if (change) return <MemoryChip key={key} change={change} />
            if (!turn.results[part.id] && (streaming || running)) return null
          }
          if (part.name === DOCUMENT_TOOL) {
            // A document written or changed is part of the reply itself; one
            // being written shows as that; reading or listing is a step.
            const result = turn.results[part.id]
            const change = documentChangeOf(result)
            const args = (part.arguments ?? {}) as { op?: unknown }
            const writing = !result && (streaming || running) && (args.op === undefined || args.op === 'create' || args.op === 'update')
            if (change || writing) return <DocumentCard key={key} change={change} call={part} />
          }
          if (part.name === SUBAGENT_TOOL) {
            // The sub-agents a response launched are one line of the reply,
            // drawn where the first of them is; one that could not be
            // launched is a failed step of the activity instead.
            const launches = launchesOf(parts, turn.results)
            if (!launches.find((l) => l.callId === part.id)?.failed) {
              const shown = launches.filter((l) => !l.failed)
              if (shown[0]?.callId !== part.id || !conversationId) return null
              return <SubagentLaunchLine key={key} conversationId={conversationId} launches={shown} streaming={streaming && isLast} />
            }
          }
          if (part.name === TASK_TOOL) {
            // A task scheduled or changed is a line of the reply, like a note
            // kept; a list, or a call that failed, is a step.
            const change = taskChangeOf(turn.results[part.id])
            if (change) return <TaskChip key={key} change={change} />
          }
          return { key, step: { call: part, result: turn.results[part.id], status: toolStatus(part.id) } }
        }
        default:
          return null
      }
    })

  const entries: Block[] = []
  // Parts are keyed by where they sit — which response of the reply, which
  // part of it — not by the response's id: the response being written has no
  // id yet, and keyed by its place it is the same element once it is saved
  // (and again once the run hands over to the message list), so the text
  // keeps its node and finishes revealing what it holds rather than being
  // drawn anew all at once (see `Markdown`).
  // Sub-agents' reports delivered while the reply was being written are not
  // drawn: the launch line and the side panel already say how each stands.
  for (const [position, response] of turn.responses.entries()) {
    const arrived = Boolean(live?.fresh.includes(response.id))
    entries.push(...renderParts(response.parts, `r${position}`, false, () => arrived))
    if (response.stopReason === 'error') {
      entries.push(
        <Notice key={`${response.id}:error`} tone="error">
          {errorText(t, response.errorCode)}
          {response.runId && <ErrorId id={response.runId.slice(0, 8)} />}
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
    // The conversation's summary ends here, and the run went on from it.
    if (turn.inside?.index === position + 1) {
      entries.push(
        <CompactionDivider key={`${response.id}:compaction`} summary={turn.inside.summary} {...divider} />,
      )
    }
  }
  const partialKey = `r${turn.responses.length}`
  if (turn.partial) entries.push(...renderParts(turn.partial, partialKey, running, (index) => index >= (live?.freshFrom ?? Infinity)))
  // A failed request being tried again is said by the line that says the
  // model is at work - the thinking line, or the activity line once there is
  // one - not by a notice of its own stacked above it.
  const retrying = running && Boolean(live?.retry)

  // One activity line per reply. Everything from the first tool call to the
  // last — the calls, and whatever the model said or thought between rounds,
  // which is narration of the search rather than the answer — folds into it,
  // in order. What comes before stays as written; what comes after is the
  // answer. A notice inside that stretch is shown just after the line.
  const first = entries.findIndex(isToolBlock)
  let last = entries.length - 1 - [...entries].reverse().findIndex(isToolBlock)
  // Thinking after the last call is still part of the search — the model
  // weighing what it found, and often deciding to look again — so it folds in
  // too. Only the answer's text comes after the line.
  if (first >= 0) {
    for (let i = last + 1; i < entries.length; i++) {
      const entry = entries[i]
      if (entry === null) continue
      if (!isFoldBlock(entry) || entry.fold.kind !== 'reasoning') break
      last = i
    }
  }
  // Working, with nothing on screen moving: before the first token, and
  // between a tool round and the next response.
  const toolRunning = Object.values(live?.tools ?? {}).includes('running')
  const waiting = running && !toolRunning && (!turn.partial || turn.partial.length === 0)
  // The model is thinking inside the line right now — streaming its thoughts,
  // or quiet between a round and its next response — and the line says so,
  // rather than reading as a search that is over with a stray caret below it.
  const tail = first >= 0 ? entries[last] : null
  const compacting = running && Boolean(live?.compacting)
  const reasoning =
    running &&
    isFoldBlock(tail) &&
    tail.fold.kind === 'reasoning' &&
    tail.key.startsWith(`${partialKey}:`) &&
    // Still the newest thing written: once the answer starts, it is over.
    entries.slice(last + 1).every((entry) => entry === null)
  // Streaming thoughts is thinking; quiet between a round and its next
  // response is only waiting, and the line keeps saying what was done last.
  const thinking =
    compacting || first < 0 ? false : retrying ? 'retrying' : reasoning ? 'reasoning' : waiting ? 'waiting' : false
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
    if (i === last) blocks.push(<ToolActivity key="activity" items={items} thinking={thinking} />, ...after)
  })

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

      {compacting ? (
        <CompactingLine />
      ) : (
        (waiting || retrying) && first < 0 && <ThinkingLine label={retrying ? t('chat.retrying') : undefined} />
      )}

      {!running && (text || onRegenerate) && (
        // The last answer keeps its actions in view, as the place to go next.
        <div className={onRegenerate ? 'n-actions always' : 'n-actions'}>
          {text && <CopyButton content={text} />}
          {onRegenerate && (
            <button type="button" className="n-icon-btn sm" aria-label={t('chat.regenerate')} onClick={onRegenerate}>
              <RegenerateIcon />
            </button>
          )}
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

/**
 * A message content moderation refused, answered in the model's place: the
 * model's head as on any reply, and the refusal as its body. The message is in
 * the transcript (a `blocked` row) and the reply is drawn after it each time;
 * it is not a reply the model wrote and is never sent back to it.
 */
export function RefusedReply({ model, modelId, refusal }: { model?: ChatModel; modelId: string; refusal: Refusal }) {
  const name = model?.name ?? modelId
  return (
    <div className="n-msg-assistant">
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
      <div className="n-msg-body">
        <RefusalCard refusal={refusal} />
      </div>
    </div>
  )
}

/**
 * The reply to a message that is still on its way to the server: the model's
 * header and the thinking line, exactly as a run looks before its first token,
 * so nothing shifts when the real one takes its place.
 */
export function PendingReply({ model, modelId }: { model?: ChatModel; modelId: string }) {
  const name = model?.name ?? modelId
  return (
    <div className="n-msg-assistant">
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
      <div className="n-msg-body" />
      <ThinkingLine />
    </div>
  )
}
