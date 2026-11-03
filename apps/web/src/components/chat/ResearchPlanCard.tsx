import { Check, CircleDashed, Telescope } from 'lucide-react'
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { PLAN_COUNTDOWN_SECONDS, planSecondsLeft, type PlanDecision, type ResearchPlan } from '@openchat/chat-core/research-plan'
import { cn } from '@/lib/utils'

export type PlanState = 'pending' | PlanDecision | 'skipped'

interface ResearchPlanCardProps {
  plan: ResearchPlan
  state: PlanState
  /** When the reply that put the plan was written: the countdown runs from it. */
  writtenAt: string
  /** A decision is on its way. */
  busy?: boolean
  /** The person is writing what to change: the countdown stops. */
  editing?: boolean
  onStart?: () => void
  onCancel?: () => void
  onEdit?: () => void
}

/**
 * A deep research's plan, in the reply that put it (docs/design/deep-research-plan.md
 * §6, after ChatGPT's): the title and a line per step, then 编辑, 取消 and
 * 开始. 开始 counts down from a minute and starts the research by itself at the
 * end, unless the person is editing; a plan come back to after that waits to
 * be started. Decided, the card stays as the record of the plan and says what
 * became of it.
 */
export default function ResearchPlanCard({ plan, state, writtenAt, busy, editing, onStart, onCancel, onEdit }: ResearchPlanCardProps) {
  const { t } = useTranslation()
  const pending = state === 'pending'
  const left = useCountdown(writtenAt, pending && !editing && !busy, onStart)

  return (
    <section className={cn('n-plan', !pending && 'is-decided')} aria-label={t('chat.research.plan')}>
      <h3 className="n-plan-title">{plan.title}</h3>
      <ol className="n-plan-steps">
        {plan.steps.map((step, i) => (
          <li key={i} className="n-plan-step">
            <CircleDashed className="n-plan-dot" aria-hidden="true" />
            <span>{step}</span>
          </li>
        ))}
      </ol>
      {pending ? (
        <div className="n-plan-actions">
          <button type="button" className="n-plan-btn" disabled={busy} onClick={onEdit}>
            {t('chat.research.edit')}
          </button>
          <div className="grow" />
          <button type="button" className="n-plan-btn" disabled={busy} onClick={onCancel}>
            {t('chat.research.cancel')}
          </button>
          <button
            type="button"
            className="n-plan-btn is-primary"
            disabled={busy}
            onClick={onStart}
            aria-describedby={left > 0 && !editing ? `${writtenAt}-countdown` : undefined}
          >
            {t('chat.research.start')}
            {left > 0 && !editing && (
              <span className="n-plan-ring" aria-hidden="true">
                <svg viewBox="0 0 24 24">
                  <circle cx="12" cy="12" r="10.5" pathLength={1} strokeDasharray={`${left / PLAN_COUNTDOWN_SECONDS} 1`} />
                </svg>
                <span>{left}</span>
              </span>
            )}
          </button>
          {left > 0 && !editing && (
            <span id={`${writtenAt}-countdown`} className="sr-only">
              {t('chat.research.starts-in', { count: left })}
            </span>
          )}
        </div>
      ) : (
        <p className="n-plan-status">
          {state === 'started' ? <Telescope aria-hidden="true" /> : state === 'revised' ? <Check aria-hidden="true" /> : null}
          {t(`chat.research.${state}`)}
        </p>
      )}
    </section>
  )
}

/**
 * Seconds left of the plan's countdown, ticking while `running`. Reaching zero
 * while it ticked on this page starts the research; a page opened after the
 * minute never counted, so it starts nothing by itself.
 */
function useCountdown(writtenAt: string, running: boolean, onDone?: () => void): number {
  const [now, setNow] = useState(() => Date.now())
  const left = planSecondsLeft(writtenAt, now)
  useEffect(() => {
    if (!running || planSecondsLeft(writtenAt, Date.now()) === 0) return
    const timer = window.setInterval(() => {
      const at = Date.now()
      setNow(at)
      if (planSecondsLeft(writtenAt, at) === 0) {
        window.clearInterval(timer)
        onDone?.()
      }
    }, 1000)
    return () => window.clearInterval(timer)
  }, [writtenAt, running, onDone])
  return running ? left : 0
}
