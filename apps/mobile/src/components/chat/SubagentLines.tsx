import { ChevronRight } from 'lucide-react-native'
import { useTranslation } from 'react-i18next'
import { Pressable, StyleSheet, Text, View } from 'react-native'

import { joinNames, launchProgress, type Launch, type ReportNotice, type ReportState } from '@openchat/chat-core/subagents'

import { useDuration, useSubagentAction, useSubagents } from '@/hooks/subagents'
import { useSubagentPanel } from '@/lib/subagent-panel'
import { useColors } from '@/theme'
import { SubagentMark, type MarkState } from './SubagentMark'

/**
 * The one line a reply shows where the main agent launched sub-agents (the
 * web's SubagentLaunchLine), kept up to date: their marks, and words that
 * follow them - the names while none has ended, then how many are working,
 * then that they are done and how long it took. A tap opens the panel (at the
 * sub-agent when there is one). While the reply waits on one, 「转到后台」
 * lets it end so the person can write again.
 */
export function SubagentLaunchLine({ conversationId, launches, streaming }: { conversationId: string; launches: Launch[]; streaming: boolean }) {
  const { t, i18n } = useTranslation()
  const colors = useColors()
  const open = useSubagentPanel((s) => s.open)
  const list = useSubagents(conversationId).data?.items
  const action = useSubagentAction(conversationId)
  const duration = useDuration()
  const named = launches.filter((l) => l.name)
  const waiting = launches.filter((l) => l.waiting)
  const known = (id: string) => list?.find((a) => a.id === id)
  const markOf = (id: string): MarkState => {
    const agent = known(id)
    const launch = launches.find((l) => l.id === id || l.callId === id)
    const state = agent?.state ?? launch?.done?.state
    if (state === 'queued' || state === 'running') return state
    if (state === 'failed' || state === 'cancelled') return 'faded'
    if (state === 'completed') return 'done'
    return launch?.waiting || (launch?.background && launch.launched) || (streaming && !named.length) ? 'running' : 'done'
  }
  // A foreground call has no sub-agent id until it returns: found by the call that launched it.
  const idOf = (launch: Launch) => list?.find((a) => a.callId === launch.callId)?.id ?? (launch.launched ? launch.id : null)
  const target = launches.length === 1 ? idOf(launches[0]!) : null
  const progress = launchProgress(launches, list)
  const extras = (p: { done?: number; queued?: number; stopped: number; failed: number }) =>
    [
      p.done ? t('subagents.launch.extra-done', { count: p.done }) : '',
      p.queued ? t('subagents.launch.extra-queued', { count: p.queued }) : '',
      p.stopped ? t('subagents.launch.extra-stopped', { count: p.stopped }) : '',
      p.failed ? t('subagents.launch.extra-failed', { count: p.failed }) : '',
    ].join('')
  let label: string
  if (named.length === 0) {
    label = t('subagents.launch.preparing')
  } else if (progress.kind === 'names') {
    const startedText = progress.started.length > 0 ? t('subagents.launch.started', { names: joinNames(progress.started, i18n.language) }) : ''
    const queuedText = progress.queued.length > 0 ? t('subagents.launch.queued', { names: joinNames(progress.queued, i18n.language) }) : ''
    label = startedText && queuedText ? t('subagents.launch.mixed', { started: startedText, queued: queuedText }) : startedText || queuedText
  } else if (progress.kind === 'progress') {
    label = t('subagents.launch.progress', { total: progress.total }) + extras(progress)
  } else if (progress.single) {
    label = `${t(`subagents.done.${progress.single.state}`, { name: progress.single.name })} · ${duration(progress.durationMs)}`
  } else {
    const whole = progress.stopped + progress.failed === 0 ? 'finished' : 'ended'
    label = t(`subagents.launch.${whole}`, { total: progress.total, duration: duration(progress.durationMs) }) + extras(progress)
  }
  const moving = waiting.map(idOf).filter((id): id is string => {
    const state = id ? known(id)?.state : undefined
    return state === 'running' || state === 'queued'
  })
  return (
    <View style={styles.line}>
      <Pressable style={styles.main} onPress={() => open(conversationId, target)} accessibilityLabel={t('subagents.open')}>
        <View style={styles.marks}>
          {launches.map((l) => {
            const id = idOf(l) ?? l.callId
            return <SubagentMark key={id} id={id} state={markOf(id)} />
          })}
        </View>
        <Text style={[styles.text, { color: colors.text2 }]} numberOfLines={2}>
          {label}
        </Text>
        <ChevronRight size={15} color={colors.text3} />
      </Pressable>
      {moving.length > 0 ? (
        <Pressable disabled={action.isPending} onPress={() => moving.forEach((id) => action.mutate({ action: 'background', id }))}>
          <Text style={[styles.action, { color: colors.text }]}>{t('subagents.launch.to-background')}</Text>
        </Pressable>
      ) : null}
    </View>
  )
}

/** Where reports were delivered: a line per report, how it ended and how long it took; a tap shows its work. */
export function SubagentReportLines({ conversationId, notices }: { conversationId: string; notices: ReportNotice[] }) {
  return (
    <View style={styles.reports}>
      {notices.map((notice) => (
        <DoneLine key={notice.id} conversationId={conversationId} id={notice.id} name={notice.name} state={notice.state} durationMs={notice.durationMs} />
      ))}
    </View>
  )
}

function DoneLine({ conversationId, id, name, state, durationMs }: { conversationId: string; id: string; name: string; state: ReportState; durationMs: number }) {
  const { t } = useTranslation()
  const colors = useColors()
  const list = useSubagents(conversationId).data?.items
  const open = useSubagentPanel((s) => s.open)
  const duration = useDuration()
  // Turning again if it was continued since.
  const mark: MarkState = list?.find((a) => a.id === id)?.state === 'running' ? 'running' : state === 'completed' ? 'done' : 'faded'
  return (
    <Pressable style={styles.main} onPress={() => open(conversationId, id)}>
      <SubagentMark id={id} state={mark} />
      <Text style={[styles.text, { color: colors.text2 }]} numberOfLines={2}>
        {t(`subagents.done.${state}`, { name })}
        <Text style={{ color: colors.text3 }}>
          {` · ${duration(durationMs)}`}
          {state !== 'completed' ? ` · ${t('subagents.done.partial')}` : ''}
        </Text>
      </Text>
    </Pressable>
  )
}

const styles = StyleSheet.create({
  line: { gap: 6 },
  main: { flexDirection: 'row', alignItems: 'center', gap: 8, minHeight: 28 },
  marks: { flexDirection: 'row', gap: 2 },
  text: { flexShrink: 1, fontSize: 14, lineHeight: 20 },
  action: { fontSize: 13, fontWeight: '500', textDecorationLine: 'underline' },
  reports: { gap: 6, alignItems: 'center' },
})
