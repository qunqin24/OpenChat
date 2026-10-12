import { Check, ChevronLeft, MessageCircleQuestion, Pencil, X } from 'lucide-react'
import { useEffect, useId, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'

import type { AskAnswer, AskQuestion } from '@/lib/ask-user'
import { cn } from '@/lib/utils'

interface AskUserDockProps {
  questions: AskQuestion[]
  /** The answers are on their way to the server. */
  busy?: boolean
  onSubmit: (answers: AskAnswer[]) => void
  /** Put the panel away; the question stays open in the reply. */
  onDismiss: () => void
  /** Put away. It stays mounted so what was chosen is still there when it comes back. */
  hidden?: boolean
}

/**
 * The model's open question, floating over the composer: one question at a
 * time, its options numbered (and on the number keys), a line to write an
 * answer of one's own, and Skip / Send. Several questions step through with
 * Next; Send goes out with the last one.
 *
 * Skip only puts the panel away. The question stays open in the reply, and a
 * message typed instead answers it as "written past", as before.
 */
export default function AskUserDock({ questions, busy, onSubmit, onDismiss, hidden }: AskUserDockProps) {
  const { t } = useTranslation()
  const id = useId()
  const [step, setStep] = useState(0)
  const [picks, setPicks] = useState<string[][]>(() => questions.map(() => []))
  const [others, setOthers] = useState<string[]>(() => questions.map(() => ''))
  const panel = useRef<HTMLElement>(null)

  const q = questions[step]!
  const multi = q.multiSelect
  const answered = (i: number) => picks[i]!.length > 0 || others[i]!.trim() !== ''
  const last = step === questions.length - 1
  const canSend = questions.every((_, i) => answered(i)) && !busy

  const choose = (label: string) => {
    setPicks((held) =>
      held.map((chosen, i) => {
        if (i !== step) return chosen
        if (multi) return chosen.includes(label) ? chosen.filter((l) => l !== label) : [...chosen, label]
        return chosen.includes(label) ? [] : [label]
      }),
    )
    // One answer for a question that takes one: a choice replaces what was written.
    if (!multi) setOthers((held) => held.map((value, i) => (i === step ? '' : value)))
  }
  const write = (text: string) => {
    setOthers((held) => held.map((value, i) => (i === step ? text : value)))
    if (text.trim() !== '' && !multi) setPicks((held) => held.map((chosen, i) => (i === step ? [] : chosen)))
  }

  const send = () => {
    if (!canSend) return
    onSubmit(
      questions.map((_, i) => {
        const other = others[i]!.trim()
        return { selected: picks[i]!, ...(other ? { other } : {}) }
      }),
    )
  }
  const advance = () => {
    if (!answered(step)) return
    if (last) send()
    else setStep(step + 1)
  }

  // Number keys pick an option while the panel has focus and no field is being
  // typed in; Escape puts the panel away.
  useEffect(() => {
    const onKey = (event: KeyboardEvent) => {
      if (hidden) return
      if (!panel.current?.contains(document.activeElement) && document.activeElement !== document.body) return
      if (event.key === 'Escape') {
        onDismiss()
        return
      }
      if (event.metaKey || event.ctrlKey || event.altKey || busy) return
      if (document.activeElement instanceof HTMLInputElement) return
      const n = Number(event.key)
      if (Number.isInteger(n) && n >= 1 && n <= q.options.length) {
        event.preventDefault()
        choose(q.options[n - 1]!.label)
      }
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  })

  const titleId = `${id}-title`
  return (
    <section ref={panel} className="n-ask-dock" role="dialog" aria-modal="false" aria-labelledby={titleId} hidden={hidden}>
      <header className="n-ask-dock-head">
        {questions.length > 1 && step > 0 ? (
          <button type="button" className="n-icon-btn sm" aria-label={t('chat.ask.previous')} onClick={() => setStep(step - 1)}>
            <ChevronLeft className="h-4 w-4" />
          </button>
        ) : (
          <MessageCircleQuestion className="n-ask-icon" aria-hidden="true" />
        )}
        <span className="n-ask-dock-label">
          {q.header || t('chat.ask.question')}
          {questions.length > 1 && <span className="n-ask-dock-count">{t('chat.ask.step', { n: step + 1, total: questions.length })}</span>}
        </span>
        <button type="button" className="n-icon-btn sm n-ask-dock-close" aria-label={t('chat.ask.dismiss')} onClick={onDismiss}>
          <X className="h-4 w-4" />
        </button>
      </header>

      <p id={titleId} className="n-ask-dock-question">
        {q.question}
        {multi && <span className="n-ask-dock-kind">{t('chat.ask.multi')}</span>}
      </p>

      <div className="n-ask-dock-options" role={multi ? 'group' : 'radiogroup'} aria-labelledby={titleId}>
        {q.options.map((option, n) => {
          const on = picks[step]!.includes(option.label)
          return (
            <button
              key={option.label}
              type="button"
              role={multi ? 'checkbox' : 'radio'}
              aria-checked={on}
              aria-keyshortcuts={n < 9 ? String(n + 1) : undefined}
              disabled={busy}
              className={cn('n-ask-dock-opt', on && 'is-on')}
              onClick={() => choose(option.label)}
            >
              <span className="n-ask-dock-num" aria-hidden="true">
                {on ? <Check className="h-3.5 w-3.5" strokeWidth={2.5} /> : n + 1}
              </span>
              <span className="n-ask-dock-opt-body">
                <span className="n-ask-dock-opt-label">{option.label}</span>
                {option.description && <span className="n-ask-dock-opt-desc">{option.description}</span>}
              </span>
            </button>
          )
        })}
      </div>

      <div className="n-ask-dock-foot">
        <label className={cn('n-ask-dock-write', others[step]!.trim() !== '' && 'is-on')}>
          <Pencil className="h-4 w-4" aria-hidden="true" />
          <input
            key={step}
            type="text"
            value={others[step]}
            disabled={busy}
            maxLength={2000}
            placeholder={t('chat.ask.write')}
            aria-label={t('chat.ask.write')}
            onChange={(e) => write(e.target.value)}
            onKeyDown={(e) => {
              // Enter confirms an IME composition first; it only moves on once that is over.
              if (e.key === 'Enter' && !e.nativeEvent.isComposing) {
                e.preventDefault()
                advance()
              }
            }}
          />
        </label>
        <button type="button" className="n-ask-dock-skip" onClick={onDismiss}>
          {t('chat.ask.skip')}
        </button>
        {last ? (
          <button type="button" className="n-ask-dock-send" disabled={!canSend} onClick={send}>
            {t('chat.ask.send')}
          </button>
        ) : (
          <button type="button" className="n-ask-dock-send" disabled={!answered(step) || busy} onClick={advance}>
            {t('chat.ask.next')}
          </button>
        )}
      </div>
    </section>
  )
}
