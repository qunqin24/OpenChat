import type { Message, Subagent, SubagentDetail } from '@openchat/api-client'
import { ArrowLeft, X } from 'lucide-react-native'
import { useEffect, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Modal, Pressable, ScrollView, StyleSheet, Text, View } from 'react-native'
import { useSafeAreaInsets } from 'react-native-safe-area-context'

import { fromSnapshot } from '@openchat/chat-core/run-state'
import { relativeAge, tally } from '@openchat/chat-core/subagents'
import { buildTurns } from '@openchat/chat-core/transcript'

import { useChatModels } from '@/hooks/models'
import { useDuration, useSubagent, useSubagentAction, useSubagents } from '@/hooks/subagents'
import { useSubagentPanel } from '@/lib/subagent-panel'
import { useColors } from '@/theme'
import { Reply } from './Reply'
import { SubagentMark, type MarkState } from './SubagentMark'

/** How often the relative times of the list move on. */
const TICK_MS = 30_000

/**
 * A conversation's sub-agents, full screen (the web's SubagentPanel): the list
 * - working ones, then finished ones - and, for one of them, its whole work
 * read-only (task, thinking, tool steps, report), kept current while it works.
 * There is no input: a sub-agent takes instructions from the main agent only.
 */
export function SubagentPanel() {
  const conversationId = useSubagentPanel((s) => s.conversationId)
  if (!conversationId) return null
  return <Panel conversationId={conversationId} />
}

function Panel({ conversationId }: { conversationId: string }) {
  const { t } = useTranslation()
  const colors = useColors()
  const insets = useSafeAreaInsets()
  const { selected, select, close } = useSubagentPanel()
  const list = useSubagents(conversationId)
  const agent = selected ? list.data?.items.find((a) => a.id === selected) : undefined
  return (
    <Modal visible animationType="slide" onRequestClose={() => (selected ? select(null) : close())} statusBarTranslucent navigationBarTranslucent>
      <View style={[styles.page, { backgroundColor: colors.bg, paddingTop: insets.top, paddingBottom: insets.bottom }]}>
        <View style={[styles.bar, { borderBottomColor: colors.line }]}>
          {selected ? (
            <Pressable hitSlop={10} onPress={() => select(null)} accessibilityLabel={t('subagents.panel.back')}>
              <ArrowLeft size={22} color={colors.text} />
            </Pressable>
          ) : null}
          {agent ? <SubagentMark id={agent.id} state={markState(agent)} size={22} /> : null}
          <Text style={[styles.title, { color: colors.text }]} numberOfLines={1}>
            {agent?.name ?? t('subagents.panel.title')}
          </Text>
          <Pressable hitSlop={10} onPress={close} accessibilityLabel={t('subagents.panel.close')}>
            <X size={22} color={colors.text} />
          </Pressable>
        </View>
        {selected ? (
          <SubagentView conversationId={conversationId} subagentId={selected} />
        ) : (
          <SubagentList conversationId={conversationId} items={list.data?.items} loading={list.isPending} onPick={select} />
        )}
      </View>
    </Modal>
  )
}

function SubagentList({ conversationId, items, loading, onPick }: { conversationId: string; items: Subagent[] | undefined; loading: boolean; onPick: (id: string) => void }) {
  const { t } = useTranslation()
  const colors = useColors()
  const now = useNow()
  if (loading) return <Text style={[styles.empty, { color: colors.text3 }]}>{t('subagents.panel.loading')}</Text>
  if (!items || items.length === 0) return <Text style={[styles.empty, { color: colors.text3 }]}>{t('subagents.panel.empty')}</Text>
  const { active, running, queued, finished } = tally(items)
  return (
    <ScrollView contentContainerStyle={styles.list}>
      {active.length > 0 ? (
        <Text style={[styles.group, { color: colors.text3 }]}>
          {queued.length > 0 ? t('subagents.panel.running-queued', { count: running.length, queued: queued.length }) : t('subagents.panel.running', { count: running.length })}
        </Text>
      ) : null}
      {active.map((agent) => (
        <Row key={agent.id} conversationId={conversationId} agent={agent} now={now} onPick={onPick} />
      ))}
      {finished.length > 0 ? <Text style={[styles.group, { color: colors.text3 }]}>{t('subagents.panel.finished', { count: finished.length })}</Text> : null}
      {finished.map((agent) => (
        <Row key={agent.id} conversationId={conversationId} agent={agent} now={now} onPick={onPick} />
      ))}
    </ScrollView>
  )
}

function Row({ conversationId, agent, now, onPick }: { conversationId: string; agent: Subagent; now: number; onPick: (id: string) => void }) {
  const { t } = useTranslation()
  const colors = useColors()
  const action = useSubagentAction(conversationId)
  const duration = useDuration()
  const waiting = agent.state === 'queued'
  const working = agent.state === 'running' || waiting
  const age = relativeAge(waiting ? agent.createdAt : working ? agent.startedAt : (agent.finishedAt ?? agent.startedAt), now)
  const doing = agent.activity ? t(`subagents.activity.${agent.activity}` as 'subagents.activity.thinking', { defaultValue: agent.activity }) : t('subagents.activity.thinking')
  const sub = waiting
    ? t('subagents.panel.queued')
    : working
      ? `${doing} · ${t('subagents.activity.calls', { count: agent.toolCalls })}`
      : [t(`subagents.state.${agent.state}`), agent.durationMs !== undefined ? duration(agent.durationMs) : null, agent.stoppedByUser ? t('subagents.panel.stopped-by-user') : null]
          .filter(Boolean)
          .join(' · ')
  return (
    <View style={styles.row}>
      <Pressable style={styles.rowMain} onPress={() => onPick(agent.id)}>
        <SubagentMark id={agent.id} state={markState(agent)} size={28} />
        <View style={styles.rowText}>
          <Text style={[styles.rowName, { color: colors.text }]} numberOfLines={1}>
            {agent.name}
          </Text>
          <Text style={[styles.rowSub, { color: colors.text3 }]} numberOfLines={1}>
            {sub}
          </Text>
        </View>
        <Text style={[styles.rowSub, { color: colors.text3 }]}>{age.unit === 'now' ? t('subagents.age.now') : t(`subagents.age.${age.unit}`, { count: age.count })}</Text>
      </Pressable>
      {working ? (
        // For a queued one, stopping is giving up its place.
        <Pressable disabled={action.isPending} onPress={() => action.mutate({ action: 'stop', id: agent.id })}>
          <Text style={[styles.action, { color: colors.text }]}>{waiting ? t('subagents.panel.cancel') : t('subagents.panel.stop')}</Text>
        </Pressable>
      ) : agent.stoppedByUser ? (
        <Pressable disabled={action.isPending} onPress={() => action.mutate({ action: 'resume', id: agent.id })}>
          <Text style={[styles.action, { color: colors.text }]}>{t('subagents.panel.resume')}</Text>
        </Pressable>
      ) : null}
    </View>
  )
}

/** One sub-agent's work, read-only, drawn with the chat's own reply. */
function SubagentView({ conversationId, subagentId }: { conversationId: string; subagentId: string }) {
  const { t } = useTranslation()
  const colors = useColors()
  const detail = useSubagent(conversationId, subagentId)
  const models = useChatModels()
  const turns = useMemo(() => (detail.data ? turnsOf(detail.data) : []), [detail.data])
  if (detail.isPending) return <Text style={[styles.empty, { color: colors.text3 }]}>{t('subagents.panel.loading')}</Text>
  if (!detail.data) return <Text style={[styles.empty, { color: colors.text3 }]}>{t('subagents.error.generic')}</Text>
  const model = detail.data.subagent.model
  let tasks = 0
  return (
    <ScrollView contentContainerStyle={styles.view}>
      {turns.map((turn) => {
        if (turn.kind === 'user') return <AgentInput key={turn.key} message={turn.message} first={tasks++ === 0} />
        if (turn.kind !== 'assistant') return null
        const modelId = turn.live?.run.model ?? turn.responses.find((m) => m.model)?.model ?? model
        return <Reply key={turn.key} turn={turn} modelId={modelId} model={models.data?.find((m) => m.id === modelId)} />
      })}
    </ScrollView>
  )
}

/** The main agent's task, or a later message of its; a long one folded to a few lines. */
function AgentInput({ message, first }: { message: Message; first: boolean }) {
  const { t } = useTranslation()
  const colors = useColors()
  const [open, setOpen] = useState(false)
  const long = message.content.length > 280
  return (
    <View style={[styles.input, { backgroundColor: colors.bubble }]}>
      <Text style={[styles.inputLabel, { color: colors.text3 }]}>{first ? t('subagents.panel.task') : t('subagents.panel.message')}</Text>
      <Text style={[styles.inputText, { color: colors.text }]} numberOfLines={long && !open ? 6 : undefined} selectable>
        {message.content}
      </Text>
      {long ? (
        <Pressable onPress={() => setOpen(!open)}>
          <Text style={[styles.action, { color: colors.text2 }]}>{open ? t('subagents.panel.show-less') : t('subagents.panel.show-more')}</Text>
        </Pressable>
      ) : null}
    </View>
  )
}

/** The view's turns: the transcript, with the run going drawn as the chat draws one. */
function turnsOf(detail: SubagentDetail) {
  const run = detail.run
  const live = run ? fromSnapshot({ type: 'snapshot', run, live: detail.live, messages: detail.messages.filter((m) => m.runId === run.id) }).run : null
  return buildTurns(detail.messages, live)
}

function markState(agent: Subagent): MarkState {
  if (agent.state === 'running') return 'running'
  if (agent.state === 'queued') return 'queued'
  return agent.state === 'completed' ? 'done' : 'faded'
}

function useNow() {
  const [now, setNow] = useState(() => Date.now())
  useEffect(() => {
    const timer = setInterval(() => setNow(Date.now()), TICK_MS)
    return () => clearInterval(timer)
  }, [])
  return now
}

const styles = StyleSheet.create({
  page: { flex: 1 },
  bar: { flexDirection: 'row', alignItems: 'center', gap: 12, paddingHorizontal: 16, paddingVertical: 12, borderBottomWidth: StyleSheet.hairlineWidth },
  title: { flex: 1, fontSize: 16, fontWeight: '500' },
  empty: { padding: 32, textAlign: 'center', fontSize: 14 },
  list: { padding: 12, gap: 4 },
  group: { fontSize: 12, marginTop: 12, marginBottom: 4, paddingHorizontal: 6 },
  row: { flexDirection: 'row', alignItems: 'center', gap: 10, paddingVertical: 10, paddingHorizontal: 6 },
  rowMain: { flex: 1, flexDirection: 'row', alignItems: 'center', gap: 12 },
  rowText: { flex: 1, gap: 2 },
  rowName: { fontSize: 15, fontWeight: '500' },
  rowSub: { fontSize: 12 },
  action: { fontSize: 13, fontWeight: '500' },
  view: { padding: 18, gap: 20 },
  input: { borderRadius: 16, padding: 12, gap: 6 },
  inputLabel: { fontSize: 12 },
  inputText: { fontSize: 15, lineHeight: 22 },
})
