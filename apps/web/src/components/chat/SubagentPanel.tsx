import type { Message, Subagent, SubagentDetail } from '@openchat/api-client'
import { ArrowLeft, X } from 'lucide-react'
import { useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { useChatModels } from '@/hooks/use-chat-models'
import { useDuration, useSubagent, useSubagentAction, useSubagents } from '@/hooks/use-subagents'
import { fromSnapshot } from '@openchat/chat-core/run-state'
import { relativeAge, tally } from '@openchat/chat-core/subagents'
import { buildTurns } from '@openchat/chat-core/transcript'
import { cn } from '@/lib/utils'
import { useSubagentPanel } from '@/stores/subagent-panel-store'
import { AssistantTurn } from './MessageItem'
import { SubagentAvatar, SubagentsIcon, type MarkState } from './SubagentAvatar'

/** How often the relative times of the list move on. */
const TICK_MS = 30_000

/**
 * The side panel of a conversation's sub-agents, where a writing block would
 * be: the list - working ones, then finished ones - and, for one of them, its
 * whole work read-only (task, thinking, tool steps, report), drawn with the
 * chat's own message components and kept current while it works. There is no
 * input: a sub-agent takes instructions from the main agent only.
 */
export function SubagentPanel({ conversationId }: { conversationId: string }) {
  const { t } = useTranslation()
  const { selected, select, close } = useSubagentPanel()
  const list = useSubagents(conversationId, true)
  const agent = selected ? list.data?.items.find((a) => a.id === selected) : undefined

  return (
    <aside className="n-subagent-panel" aria-label={t('subagents.panel.title')}>
      <header className="n-doc-bar n-subagent-bar">
        {selected ? (
          <div className="n-subagent-bar-who">
            <button type="button" className="n-icon-btn" aria-label={t('subagents.panel.back')} title={t('subagents.panel.back')} onClick={() => select(null)}>
              <ArrowLeft className="h-4 w-4" />
            </button>
            {agent && <SubagentAvatar id={agent.id} state={markState(agent)} size={22} />}
            <span className="n-subagent-bar-name" dir="auto">{agent?.name ?? ''}</span>
          </div>
        ) : (
          <span className="n-doc-kind"><SubagentsIcon />{t('subagents.panel.title')}</span>
        )}
        <div className="n-doc-tools">
          {agent && <ModelName id={agent.model} />}
          <button type="button" className="n-icon-btn" aria-label={t('subagents.panel.close')} title={t('subagents.panel.close')} onClick={close}>
            <X className="h-4 w-4" />
          </button>
        </div>
      </header>
      {selected ? (
        <SubagentView conversationId={conversationId} subagentId={selected} />
      ) : (
        <SubagentList conversationId={conversationId} items={list.data?.items} loading={list.isPending} onPick={select} />
      )}
    </aside>
  )
}

function ModelName({ id }: { id: string }) {
  const models = useChatModels()
  const name = models.data?.find((m) => m.id === id)?.name ?? id
  return <span className="n-subagent-model" title={id}>{name}</span>
}

function SubagentList({
  conversationId,
  items,
  loading,
  onPick,
}: {
  conversationId: string
  items: Subagent[] | undefined
  loading: boolean
  onPick: (id: string) => void
}) {
  const { t } = useTranslation()
  const now = useNow()
  if (loading) return <div className="n-subagent-empty">{t('subagents.panel.loading')}</div>
  if (!items || items.length === 0) return <div className="n-subagent-empty">{t('subagents.panel.empty')}</div>
  const { active, running, queued, finished } = tally(items)
  return (
    <div className="n-subagent-list">
      {active.length > 0 && (
        <section>
          <h3 className="n-subagent-group">
            {queued.length > 0
              ? t('subagents.panel.running-queued', { count: running.length, queued: queued.length })
              : t('subagents.panel.running', { count: running.length })}
          </h3>
          {active.map((agent) => (
            <Row key={agent.id} conversationId={conversationId} agent={agent} now={now} onPick={onPick} />
          ))}
        </section>
      )}
      {finished.length > 0 && (
        <section>
          <h3 className="n-subagent-group">{t('subagents.panel.finished', { count: finished.length })}</h3>
          {finished.map((agent) => (
            <Row key={agent.id} conversationId={conversationId} agent={agent} now={now} onPick={onPick} />
          ))}
        </section>
      )}
    </div>
  )
}

function Row({ conversationId, agent, now, onPick }: { conversationId: string; agent: Subagent; now: number; onPick: (id: string) => void }) {
  const { t } = useTranslation()
  const action = useSubagentAction(conversationId)
  const duration = useDuration()
  const waiting = agent.state === 'queued'
  const working = agent.state === 'running' || waiting
  const age = relativeAge(waiting ? agent.createdAt : working ? agent.startedAt : (agent.finishedAt ?? agent.startedAt), now)
  const doing = agent.activity ? t(`subagents.activity.${agent.activity}` as 'subagents.activity.thinking', { defaultValue: agent.activity }) : t('subagents.activity.thinking')
  return (
    <div className={cn('n-subagent-row', working && 'is-running')}>
      <button type="button" className="n-subagent-row-main" onClick={() => onPick(agent.id)}>
        <SubagentAvatar id={agent.id} state={markState(agent)} size={28} />
        <span className="n-subagent-row-text">
          <span className="n-subagent-row-name" dir="auto">{agent.name}</span>
          <span className="n-subagent-row-sub">
            {waiting
              ? t('subagents.panel.queued')
              : working
              ? `${doing} · ${t('subagents.activity.calls', { count: agent.toolCalls })}`
              : [
                  t(`subagents.state.${agent.state}`),
                  agent.durationMs !== undefined ? duration(agent.durationMs) : null,
                  agent.stoppedByUser ? t('subagents.panel.stopped-by-user') : null,
                ]
                  .filter(Boolean)
                  .join(' · ')}
          </span>
        </span>
        <span className="n-subagent-row-age">{age.unit === 'now' ? t('subagents.age.now') : t(`subagents.age.${age.unit}`, { count: age.count })}</span>
      </button>
      {working ? (
        // For a queued one, stopping is giving up its place.
        <button type="button" className="n-subagent-row-action" disabled={action.isPending} onClick={() => action.mutate({ action: 'stop', id: agent.id })}>
          {waiting ? t('subagents.panel.cancel') : t('subagents.panel.stop')}
        </button>
      ) : (
        agent.stoppedByUser && (
          <button type="button" className="n-subagent-row-action" disabled={action.isPending} onClick={() => action.mutate({ action: 'resume', id: agent.id })}>
            {t('subagents.panel.resume')}
          </button>
        )
      )}
    </div>
  )
}

/** One sub-agent's work, read-only. */
function SubagentView({ conversationId, subagentId }: { conversationId: string; subagentId: string }) {
  const { t } = useTranslation()
  const detail = useSubagent(conversationId, subagentId)
  const models = useChatModels()
  const turns = useMemo(() => (detail.data ? turnsOf(detail.data) : []), [detail.data])
  if (detail.isPending) return <div className="n-subagent-empty">{t('subagents.panel.loading')}</div>
  if (!detail.data) return <div className="n-subagent-empty">{t('subagents.error.generic')}</div>
  const model = detail.data.subagent.model
  let tasks = 0
  return (
    <div className="n-subagent-view">
      <div className="n-subagent-transcript">
        {turns.map((turn) => {
          if (turn.kind === 'user') {
            const first = tasks++ === 0
            return <AgentInput key={turn.key} message={turn.message} first={first} />
          }
          if (turn.kind !== 'assistant') return null
          const modelId = turn.live?.run.model ?? turn.responses.find((m) => m.model)?.model ?? model
          return <AssistantTurn key={turn.key} turn={turn} modelId={modelId} model={models.data?.find((m) => m.id === modelId)} />
        })}
      </div>
    </div>
  )
}

/** The main agent's task, or a later message of its, at the head of a stretch of work. */
function AgentInput({ message, first }: { message: Message; first: boolean }) {
  const { t } = useTranslation()
  return (
    <div className="n-subagent-input">
      <div className="n-subagent-input-label">{first ? t('subagents.panel.task') : t('subagents.panel.message')}</div>
      <InputText text={message.content} />
    </div>
  )
}

/**
 * A task is often a page of instructions, and the work it led to is what the
 * panel is opened for: a long one is shown to a few lines that fade out, with
 * a button to read the rest, as a long message is in Claude's chat.
 */
function InputText({ text }: { text: string }) {
  const { t } = useTranslation()
  const box = useRef<HTMLDivElement>(null)
  const [long, setLong] = useState(false)
  const [open, setOpen] = useState(false)
  useLayoutEffect(() => {
    const el = box.current
    if (!el) return
    // Measured against the clamp in subagents.css, whether or not it applies.
    const measure = () => setLong(el.scrollHeight > parseFloat(getComputedStyle(el).getPropertyValue('--n-input-clamp')) + 1)
    measure()
    const observer = new ResizeObserver(measure)
    observer.observe(el)
    return () => observer.disconnect()
  }, [text])
  return (
    <>
      <div ref={box} className={cn('n-subagent-input-text', long && !open && 'is-clamped')} dir="auto">
        {text}
      </div>
      {long && (
        <button type="button" className="n-subagent-input-more" onClick={() => setOpen(!open)} aria-expanded={open}>
          {open ? t('subagents.panel.show-less') : t('subagents.panel.show-more')}
        </button>
      )}
    </>
  )
}

/** The view's turns: the transcript, with the run going drawn as the chat draws one. */
function turnsOf(detail: SubagentDetail) {
  const run = detail.run
  const live = run
    ? fromSnapshot({
        type: 'snapshot',
        run,
        live: detail.live,
        messages: detail.messages.filter((m) => m.runId === run.id),
      }).run
    : null
  return buildTurns(detail.messages, live)
}

/** How a sub-agent's mark is drawn: never greyed, only paler when it failed or was stopped. */
function markState(agent: Subagent): MarkState {
  if (agent.state === 'running') return 'running'
  if (agent.state === 'queued') return 'queued'
  return agent.state === 'completed' ? 'done' : 'faded'
}

function useNow() {
  const [now, setNow] = useState(() => Date.now())
  useEffect(() => {
    const timer = window.setInterval(() => setNow(Date.now()), TICK_MS)
    return () => window.clearInterval(timer)
  }, [])
  return now
}

export default SubagentPanel
