import type { ChatModel, MessagePart } from '@openchat/api-client'
import * as Clipboard from 'expo-clipboard'
import { Brain, Check, ChevronDown, ChevronRight, Copy, MessageCircleQuestion, RotateCcw } from 'lucide-react-native'
import { useState, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import { Pressable, StyleSheet, Text, View } from 'react-native'

import { answersOf, ASK_USER, parseQuestions } from '@openchat/chat-core/ask-user'
import { splitSegments } from '@openchat/chat-core/genui/segments'
import { MEMORY_TOOL, memoryChangeOf } from '@openchat/chat-core/memory-tool'
import type { Refusal } from '@openchat/chat-core/moderation-refusal'
import type { ActivityItem, ActivityNote, ToolActivityStep, ToolStatus } from '@openchat/chat-core/tool-activity'
import { turnText, type AssistantTurn } from '@openchat/chat-core/transcript'

import { useColors } from '@/theme'
import { ActivityLine } from './Activity'
import { GenUIBlock } from './GenUIBlock'
import { Markdown } from './Markdown'
import { Pulse } from './Pulse'

/** Run failures with their own wording (`chat.error.*`). */
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

/** An answer's text: prose as Markdown, `openchat-ui` blocks as components. */
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
 * The model's reasoning: open while it is written, folded into how long it
 * took once it is done; either way the reader can open or close it.
 */
function Reasoning({ text, streaming, durationMs, fresh }: { text: string; streaming: boolean; durationMs?: number; fresh: boolean }) {
  const { t } = useTranslation()
  const colors = useColors()
  const [open, setOpen] = useState(streaming)
  const [wasStreaming, setWasStreaming] = useState(streaming)
  if (streaming !== wasStreaming) {
    setWasStreaming(streaming)
    setOpen(streaming)
  }
  if (!text && !streaming) return null
  const seconds = durationMs === undefined ? null : Math.max(1, Math.round(durationMs / 1000))
  const Chevron = open ? ChevronDown : ChevronRight
  return (
    <View>
      <Pressable style={styles.foldHead} onPress={() => setOpen(!open)} accessibilityRole="button" accessibilityState={{ expanded: open }}>
        {streaming ? <Pulse /> : <Brain size={15} color={colors.text3} />}
        <Text style={[styles.foldTitle, { color: colors.text2 }]}>
          {streaming ? t('chat.reasoning.live') : seconds === null ? t('chat.reasoning.done') : t('chat.reasoning.took', { count: seconds })}
        </Text>
        {text ? <Chevron size={15} color={colors.text3} /> : null}
      </Pressable>
      {open && text ? (
        <View style={[styles.reasoningBody, { borderLeftColor: colors.line }]}>
          <Markdown text={text} streaming={streaming} fresh={fresh && streaming} />
        </View>
      ) : null}
    </View>
  )
}

function Notice({ tone, children }: { tone: 'error' | 'quiet'; children: ReactNode }) {
  const colors = useColors()
  return (
    <Text style={[styles.notice, { color: tone === 'error' ? colors.errInk : colors.text3 }]} accessibilityRole={tone === 'error' ? 'alert' : undefined}>
      {children}
    </Text>
  )
}

function CopyButton({ text }: { text: string }) {
  const { t } = useTranslation()
  const colors = useColors()
  const [copied, setCopied] = useState(false)
  return (
    <Pressable
      hitSlop={8}
      style={styles.action}
      accessibilityLabel={t('chat.copy')}
      onPress={() =>
        void Clipboard.setStringAsync(text).then(() => {
          setCopied(true)
          setTimeout(() => setCopied(false), 1500)
        })
      }
    >
      {copied ? <Check size={17} color={colors.text2} /> : <Copy size={17} color={colors.text2} />}
    </Pressable>
  )
}

/** A tool call waiting to be folded into an activity line. */
type ToolBlock = { key: string; step: ToolActivityStep }
/** Text or reasoning: drawn as `node`, or folded into the activity as `fold`. */
type FoldBlock = { key: string; node: ReactNode; fold: ActivityNote }
type Block = { key: string; node: ReactNode } | ToolBlock | FoldBlock | null

const isToolBlock = (block: Block): block is ToolBlock => block !== null && 'step' in block
const isFoldBlock = (block: Block): block is FoldBlock => block !== null && 'fold' in block

interface ReplyProps {
  turn: AssistantTurn
  model?: ChatModel
  modelId: string
  /** The last answer only: answer again. */
  onRegenerate?: () => void
  /** The call id of the model's question this reply waits on, if it does. */
  askingCallId?: string | null
  /** Brings the waiting question's panel back over the composer. */
  onOpenQuestion?: () => void
}

/**
 * One reply: every response and tool step of a run, in order, under the
 * model's name. As the web's AssistantTurn (MessageItem.tsx): tool calls and
 * whatever the model said or thought between rounds fold into one activity
 * line; what comes after the last call is the answer. Parts are keyed by their
 * place (`r<n>:<i>`), never by a message id, so a part keeps its element - and
 * its reveal - across the hand-over from the live run to the message list.
 */
export function Reply({ turn, model, modelId, onRegenerate, askingCallId, onOpenQuestion }: ReplyProps) {
  const { t } = useTranslation()
  const colors = useColors()
  const live = turn.live
  const running = live?.run.status === 'running'

  const toolStatus = (callId: string): ToolStatus => {
    const result = turn.results[callId]
    if (result) return result.status
    if (live?.tools[callId] === 'running') return 'running'
    return running ? 'pending' : 'interrupted'
  }

  const renderParts = (parts: MessagePart[], prefix: string, streaming: boolean, fresh: (index: number) => boolean): Block[] =>
    parts.map((part, index): Block => {
      const key = `${prefix}:${index}`
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
            const questions = parseQuestions(part.arguments)
            const result = turn.results[part.id]
            // Refused by the server: it never reached the person.
            if (result?.status === 'failed') return null
            if (!questions) return streaming ? { key, node: <QuietLine key={key} text={t('chat.ask.preparing')} pulse /> } : null
            const answers = answersOf(result)
            const state = answers ? 'answered' : !result && (part.id === askingCallId || running) ? 'pending' : 'skipped'
            return {
              key,
              node: (
                <AskLine
                  key={key}
                  label={questions[0]?.question ?? t('chat.ask.label')}
                  state={state}
                  onOpen={state === 'pending' ? onOpenQuestion : undefined}
                />
              ),
            }
          }
          if (part.name === MEMORY_TOOL) {
            const change = memoryChangeOf(turn.results[part.id])
            if (change) return { key, node: <QuietLine key={key} text={t(`chat.memory.${change.op}`)} icon={<Brain size={14} color={colors.text3} />} /> }
            if (!turn.results[part.id] && (streaming || running)) return null
          }
          return { key, step: { call: part, result: turn.results[part.id], status: toolStatus(part.id) } }
        }
        default:
          return null
      }
    })

  const entries: Block[] = []
  for (const [position, response] of turn.responses.entries()) {
    const arrived = Boolean(live?.fresh.includes(response.id))
    entries.push(...renderParts(response.parts, `r${position}`, false, () => arrived))
    if (response.stopReason === 'error') {
      const known = ERROR_CODES.find((c) => c === response.errorCode)
      entries.push({
        key: `${response.id}:error`,
        node: (
          <Notice key={`${response.id}:error`} tone="error">
            {t(`chat.error.${known ?? 'model_error'}`)}
            {response.runId ? `  ${t('chat.error-id', { id: response.runId.slice(0, 8) })}` : ''}
          </Notice>
        ),
      })
    } else if (response.stopReason === 'cancelled') {
      entries.push({ key: `${response.id}:stopped`, node: <Notice key={`${response.id}:stopped`} tone="quiet">{t('chat.stopped')}</Notice> })
    } else if (response.stopReason === 'length') {
      entries.push({ key: `${response.id}:length`, node: <Notice key={`${response.id}:length`} tone="quiet">{t('chat.truncated')}</Notice> })
    }
  }
  const partialKey = `r${turn.responses.length}`
  if (turn.partial) entries.push(...renderParts(turn.partial, partialKey, running, (index) => index >= (live?.freshFrom ?? Infinity)))
  const retrying = running && Boolean(live?.retry)

  // One activity line per reply: everything from the first tool call to the
  // last, and the thinking right after it, folds in.
  const first = entries.findIndex(isToolBlock)
  let last = entries.length - 1 - [...entries].reverse().findIndex(isToolBlock)
  if (first >= 0) {
    for (let i = last + 1; i < entries.length; i++) {
      const entry = entries[i]
      if (entry === null || entry === undefined) continue
      if (!isFoldBlock(entry) || entry.fold.kind !== 'reasoning') break
      last = i
    }
  }
  const toolRunning = Object.values(live?.tools ?? {}).includes('running')
  const waiting = running && !toolRunning && (!turn.partial || turn.partial.length === 0)
  const tail = first >= 0 ? entries[last] : null
  const compacting = running && Boolean(live?.compacting)
  const reasoning =
    running &&
    isFoldBlock(tail ?? null) &&
    (tail as FoldBlock).fold.kind === 'reasoning' &&
    (tail as FoldBlock).key.startsWith(`${partialKey}:`) &&
    entries.slice(last + 1).every((entry) => entry === null)
  const thinking = compacting || first < 0 ? false : retrying ? 'retrying' : reasoning ? 'reasoning' : waiting ? 'waiting' : false

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
      if (entry !== null) after.push((entry as { node: ReactNode }).node)
    } else if (entry !== null && !isToolBlock(entry)) {
      blocks.push(entry.node)
    }
    if (i === last && first >= 0) blocks.push(<ActivityLine key="activity" items={items} thinking={thinking} />, ...after)
  })

  const text = turnText(turn)
  return (
    <View style={styles.reply}>
      <Text style={[styles.modelName, { color: colors.text3 }]}>{model?.name ?? modelId}</Text>
      <View style={styles.body}>{blocks}</View>
      {compacting ? (
        <QuietLine text={t('chat.compaction.running')} pulse />
      ) : (waiting || retrying) && first < 0 ? (
        <QuietLine text={retrying ? t('chat.retrying') : ''} pulse />
      ) : null}
      {!running && (text || onRegenerate) ? (
        <View style={styles.actions}>
          {text ? <CopyButton text={text} /> : null}
          {onRegenerate ? (
            <Pressable hitSlop={8} style={styles.action} accessibilityLabel={t('chat.regenerate')} onPress={onRegenerate}>
              <RotateCcw size={17} color={colors.text2} />
            </Pressable>
          ) : null}
        </View>
      ) : null}
    </View>
  )
}

/** A quiet line in a reply: a note kept, a question being prepared, the model working. */
function QuietLine({ text, pulse, icon }: { text: string; pulse?: boolean; icon?: ReactNode }) {
  const colors = useColors()
  return (
    <View style={styles.quiet}>
      {pulse ? <Pulse /> : icon}
      {text ? <Text style={[styles.quietText, { color: colors.text2 }]}>{text}</Text> : null}
    </View>
  )
}

/** The model's question, as a line in the reply; the one waiting opens its panel. */
function AskLine({ label, state, onOpen }: { label: string; state: 'answered' | 'pending' | 'skipped'; onOpen?: () => void }) {
  const { t } = useTranslation()
  const colors = useColors()
  return (
    <Pressable
      disabled={!onOpen}
      onPress={onOpen}
      style={[styles.ask, { backgroundColor: colors.bubble }]}
      accessibilityRole={onOpen ? 'button' : undefined}
    >
      <MessageCircleQuestion size={16} color={colors.text2} />
      <View style={styles.askBody}>
        <Text style={[styles.askLabel, { color: colors.text }]} numberOfLines={2}>
          {label}
        </Text>
        <Text style={[styles.askState, { color: colors.text3 }]}>
          {state === 'answered' ? t('chat.ask.answered') : state === 'skipped' ? t('chat.ask.skipped') : t('chat.ask.pending')}
        </Text>
      </View>
      {onOpen ? <Text style={[styles.askOpen, { color: colors.text }]}>{t('chat.ask.open')}</Text> : null}
    </Pressable>
  )
}

/** A message moderation refused, answered in the model's place. */
export function RefusedReply({ model, modelId, refusal }: { model?: ChatModel; modelId: string; refusal?: Refusal }) {
  const { t } = useTranslation()
  const colors = useColors()
  return (
    <View style={styles.reply}>
      <Text style={[styles.modelName, { color: colors.text3 }]}>{model?.name ?? modelId}</Text>
      <View style={[styles.refused, { backgroundColor: colors.errFill }]}>
        <Text style={[styles.refusedTitle, { color: colors.errInk }]}>{t('chat.refused.title')}</Text>
        <Text style={[styles.refusedText, { color: colors.text }]}>{t('chat.refused.text')}</Text>
        {refusal?.strikesLeft ? (
          <Text style={[styles.refusedText, { color: colors.errInk }]}>
            {t('chat.refused.warning', { count: refusal.strikesLeft, hours: refusal.windowHours })}
          </Text>
        ) : null}
      </View>
    </View>
  )
}

/** The reply to a message on its way: the model's name, and that it is at work. */
export function PendingReply({ model, modelId }: { model?: ChatModel; modelId: string }) {
  const colors = useColors()
  return (
    <View style={styles.reply}>
      <Text style={[styles.modelName, { color: colors.text3 }]}>{model?.name ?? modelId}</Text>
      <QuietLine text="" pulse />
    </View>
  )
}

const styles = StyleSheet.create({
  reply: { gap: 8 },
  modelName: { fontSize: 13, lineHeight: 18 },
  body: { gap: 12 },
  foldHead: { flexDirection: 'row', alignItems: 'center', gap: 8, minHeight: 28 },
  foldTitle: { fontSize: 14, lineHeight: 20 },
  reasoningBody: { marginTop: 6, marginLeft: 4, paddingLeft: 12, borderLeftWidth: 1, opacity: 0.75 },
  notice: { fontSize: 14, lineHeight: 21 },
  actions: { flexDirection: 'row', gap: 4, marginLeft: -6 },
  action: { padding: 6 },
  quiet: { flexDirection: 'row', alignItems: 'center', gap: 8, minHeight: 24 },
  quietText: { fontSize: 14, lineHeight: 20 },
  ask: { flexDirection: 'row', alignItems: 'center', gap: 10, padding: 12, borderRadius: 16 },
  askBody: { flex: 1, gap: 2 },
  askLabel: { fontSize: 14, lineHeight: 20 },
  askState: { fontSize: 12, lineHeight: 17 },
  askOpen: { fontSize: 14, fontWeight: '500' },
  refused: { padding: 14, borderRadius: 16, gap: 4 },
  refusedTitle: { fontSize: 14, fontWeight: '600' },
  refusedText: { fontSize: 14, lineHeight: 21 },
})
