import { ChevronRight, MessageCircleQuestion } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import type { AskAnswer, AskQuestion } from '@/lib/ask-user'
import { cn } from '@/lib/utils'

export type AskState = 'pending' | 'answered' | 'skipped'

interface AskUserCardProps {
  questions: AskQuestion[]
  state: AskState
  /** What was chosen, once answered. */
  answers?: AskAnswer[] | null
  /** Brings the open question's panel back over the composer. */
  onOpen?: () => void
}

/**
 * The model's questions, in the reply that asked them. While open they are
 * answered in a panel over the composer (AskUserDock), so here they are one
 * line pointing at it. Submitted, the card is the record of what was chosen;
 * written past (the user typed a message instead) it folds into one line that
 * says so.
 */
export default function AskUserCard({ questions, state, answers, onOpen }: AskUserCardProps) {
  if (state === 'skipped') return <SkippedCard questions={questions} />
  if (state === 'answered') return <AnsweredCard questions={questions} answers={answers ?? []} />
  return <WaitingLine questions={questions} onOpen={onOpen} />
}

/**
 * While the model is still writing the question there is nothing to put to the
 * person yet, and a reply that stops dead looks stuck. This is the one quiet
 * line that says what is coming: the question mark and a few words, pulsing in
 * opacity (no movement, so it also runs under reduced motion — a frozen
 * indicator cannot be told from a stuck one). It is replaced by the waiting
 * line the moment the call is complete.
 */
export function PreparingLine() {
  const { t } = useTranslation()
  return (
    <section className="n-ask is-preparing" aria-label={t('chat.ask.label')}>
      <div className="n-ask-fold" role="status">
        <MessageCircleQuestion className="n-ask-icon" aria-hidden="true" />
        <span className="n-ask-label n-ask-pulse" data-motion="essential">
          {t('chat.ask.preparing')}
        </span>
      </div>
    </section>
  )
}

function WaitingLine({ questions, onOpen }: { questions: AskQuestion[]; onOpen?: () => void }) {
  const { t } = useTranslation()
  const summary = questions.map((q) => q.header || q.question).join(' · ')
  return (
    <section className="n-ask is-waiting" aria-label={t('chat.ask.label')}>
      <button type="button" className="n-ask-fold" disabled={!onOpen} onClick={onOpen}>
        <MessageCircleQuestion className="n-ask-icon" aria-hidden="true" />
        <span className="n-ask-label">{t('chat.ask.pending')}</span>
        <span className="n-ask-summary">{summary}</span>
        {onOpen && <span className="n-ask-open">{t('chat.ask.open')}</span>}
      </button>
    </section>
  )
}

/** What was chosen for one question, as text. */
function chosenText(answer: AskAnswer | undefined): string {
  if (!answer) return ''
  return [...answer.selected, ...(answer.other ? [answer.other] : [])].join('、')
}

/**
 * The record of an answered question, drawn like the reply's other folded
 * lines (reasoning, search): one line saying what was chosen, which opens on
 * each question and its answer.
 */
function AnsweredCard({ questions, answers }: { questions: AskQuestion[]; answers: AskAnswer[] }) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  const summary = questions.map((_, i) => chosenText(answers[i]) || t('chat.ask.unanswered')).join(' · ')
  return (
    <section className={cn('n-ask is-answered', open && 'is-open')} aria-label={t('chat.ask.label')}>
      <button type="button" className="n-ask-fold" aria-expanded={open} onClick={() => setOpen(!open)}>
        <MessageCircleQuestion className="n-ask-icon" aria-hidden="true" />
        <span className="n-ask-label">{t('chat.ask.answered')}</span>
        <span className="n-ask-summary">{summary}</span>
        <ChevronRight className="n-ask-chevron" aria-hidden="true" />
      </button>
      {open && (
        <dl className="n-ask-record">
          {questions.map((q, i) => (
            <div key={i} className="n-ask-record-row">
              <dt>{q.question}</dt>
              <dd>{chosenText(answers[i]) || <span className="n-ask-none">{t('chat.ask.unanswered')}</span>}</dd>
            </div>
          ))}
        </dl>
      )}
    </section>
  )
}

function SkippedCard({ questions }: { questions: AskQuestion[] }) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  const summary = questions.map((q) => q.header || q.question).join(' · ')
  return (
    <section className={cn('n-ask is-skipped', open && 'is-open')} aria-label={t('chat.ask.label')}>
      <button type="button" className="n-ask-fold" aria-expanded={open} onClick={() => setOpen(!open)}>
        <MessageCircleQuestion className="n-ask-icon" aria-hidden="true" />
        <span className="n-ask-label">{t('chat.ask.skipped')}</span>
        <span className="n-ask-summary">{summary}</span>
        <ChevronRight className="n-ask-chevron" aria-hidden="true" />
      </button>
      {open && (
        <dl className="n-ask-record">
          {questions.map((q, i) => (
            <div key={i} className="n-ask-record-row">
              <dt>{q.question}</dt>
              <dd className="is-muted">{q.options.map((option) => option.label).join(' / ')}</dd>
            </div>
          ))}
        </dl>
      )}
    </section>
  )
}
