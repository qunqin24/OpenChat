import { ChevronRight } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { useDuration, useSubagentAction, useSubagents } from '@/hooks/use-subagents'
import { joinNames, launchProgress, type Launch, type ReportNotice, type ReportState } from '@openchat/chat-core/subagents'
import { useSubagentPanel } from '@/stores/subagent-panel-store'
import { SubagentAvatar, SubagentAvatars, type MarkState } from './SubagentAvatar'

/**
 * The one line a reply shows where the main agent launched sub-agents, kept up
 * to date in place: their marks side by side, each as the sub-agent stands
 * (queued, working, done, stopped), and words that follow them - the names
 * while none has ended (「资料调研、价格对比和竞品梳理开始工作」), then
 * 「4 个子智能体工作中 · 1 个已完成」, then 「4 个子智能体已完成 · 3 分 30 秒」.
 * There is no line per sub-agent: the side panel, which a click opens (at the
 * sub-agent when there is one), has each one's state and time. While a
 * foreground one is being waited for, 「转到后台」 lets the reply end so the
 * person can write again.
 */
export function SubagentLaunchLine({ conversationId, launches, streaming }: { conversationId: string; launches: Launch[]; streaming: boolean }) {
  const { t, i18n } = useTranslation()
  const open = useSubagentPanel((s) => s.open)
  const list = useSubagents(conversationId, true).data?.items
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
  // A foreground call has no sub-agent id until it returns: the panel finds it
  // by the call that launched it.
  const idOf = (launch: Launch) => list?.find((a) => a.callId === launch.callId)?.id ?? (launch.launched ? launch.id : null)
  const target = launches.length === 1 ? idOf(launches[0]!) : null
  const language = i18n.resolvedLanguage ?? 'zh'
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
    const startedText = progress.started.length > 0 ? t('subagents.launch.started', { names: joinNames(progress.started, language) }) : ''
    const queuedText = progress.queued.length > 0 ? t('subagents.launch.queued', { names: joinNames(progress.queued, language) }) : ''
    label = startedText && queuedText ? t('subagents.launch.mixed', { started: startedText, queued: queuedText }) : startedText || queuedText
  } else if (progress.kind === 'progress') {
    label = t('subagents.launch.progress', { total: progress.total }) + extras(progress)
  } else if (progress.single) {
    label = `${t(`subagents.done.${progress.single.state}`, { name: progress.single.name })} · ${duration(progress.durationMs)}`
  } else {
    const whole = progress.stopped + progress.failed === 0 ? 'finished' : 'ended'
    label = t(`subagents.launch.${whole}`, { total: progress.total, duration: duration(progress.durationMs) }) + extras(progress)
  }
  // Every one the reply waits on, queued ones too: one click lets the reply
  // end. A queued one then starts by itself when a slot frees, in the
  // background.
  const moving = waiting.map(idOf).filter((id): id is string => {
    const state = id ? known(id)?.state : undefined
    return state === 'running' || state === 'queued'
  })

  return (
    <div className="n-subagent-lines">
      <div className="n-subagent-line">
        <button type="button" className="n-subagent-line-main" onClick={() => open(conversationId, target)} aria-label={t('subagents.open')}>
          <SubagentAvatars ids={launches.map((l) => idOf(l) ?? l.callId)} state={markOf} />
          <span className="n-subagent-line-text">{label}</span>
          <ChevronRight className="n-subagent-line-go" aria-hidden="true" />
        </button>
        {moving.length > 0 && (
          <button
            type="button"
            className="n-subagent-line-action"
            disabled={action.isPending}
            onClick={() => moving.forEach((id) => action.mutate({ action: 'background', id }))}
          >
            {t('subagents.launch.to-background')}
          </button>
        )}
      </div>
    </div>
  )
}

/**
 * Where reports were delivered: one line per report - 「价格对比完成 · 3 分 12
 * 秒」, and for one that failed or was stopped that it handed back what it had.
 * The report itself is read in the sub-agent's view, not spread here. Between
 * turns (`divider`: delivered while the conversation was idle, the reply it
 * started below) each line is a divider, centred between dashed rules.
 */
export function SubagentReportLines({ conversationId, notices, divider = false }: { conversationId: string; notices: ReportNotice[]; divider?: boolean }) {
  return (
    <div className={divider ? 'n-subagent-lines is-divider' : 'n-subagent-lines'}>
      {notices.map((notice) => (
        <DoneLine key={notice.id} conversationId={conversationId} id={notice.id} name={notice.name} state={notice.state} durationMs={notice.durationMs} divider={divider} />
      ))}
    </div>
  )
}

function DoneLine({
  conversationId,
  id,
  name,
  state,
  durationMs,
  divider = false,
}: {
  conversationId: string
  id: string
  name: string
  state: ReportState
  durationMs: number
  divider?: boolean
}) {
  const list = useSubagents(conversationId, true).data?.items
  const { t } = useTranslation()
  const open = useSubagentPanel((s) => s.open)
  const duration = useDuration()
  return (
    <div className="n-subagent-line">
      <button type="button" className="n-subagent-line-main" onClick={() => open(conversationId, id)}>
        {/* Turning again if it was continued since. */}
        <SubagentAvatar id={id} state={list?.find((a) => a.id === id)?.state === 'running' ? 'running' : state === 'completed' ? 'done' : 'faded'} />
        <span className="n-subagent-line-text">
          {t(`subagents.done.${state}`, { name })}
          <span className="n-subagent-line-meta"> · {duration(durationMs)}</span>
          {state !== 'completed' && <span className="n-subagent-line-meta"> · {t('subagents.done.partial')}</span>}
        </span>
        {!divider && <ChevronRight className="n-subagent-line-go" aria-hidden="true" />}
      </button>
    </div>
  )
}
