import { ArrowRight } from 'lucide-react'
import { useContext, useId, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { choicesState } from '@/lib/genui/choices'
import type { UINode } from '@/lib/genui/resolve'
import { cn } from '@/lib/utils'
import { ChoicesContext } from './choices-context'

/** At most this many choices are drawn, as the catalogue tells the model. */
const MAX_CHOICES = 4

/**
 * A question with a few directions to take next, each sending a message
 * written for it (Choices and Choice in the catalogue). Picking does nothing on
 * its own; the button sends it, through the same path as the composer.
 *
 * Only the last answer's choices can be sent. Once the conversation has moved
 * on the card stays as a record, with the choice that was sent marked - when
 * the next message is one of its own.
 */
export function ChoicesBlock({ question, items, action, complete }: { question: string; items: UINode[]; action: string; complete: boolean }) {
  const { t } = useTranslation()
  const { pick, next } = useContext(ChoicesContext)
  const name = useId()
  const options = items
    .slice(0, MAX_CHOICES)
    .map((item) => ({ key: item.key, label: str(item.props.label), message: str(item.props.message) }))
    .filter((option) => option.label && option.message)
  const [selected, setSelected] = useState<number | null>(null)
  const { taken, open, live } = choicesState(
    options.map((option) => option.message),
    { complete, next, canPick: Boolean(pick) },
  )
  const marked = taken ?? selected

  return (
    <fieldset className={cn('n-ui-choices', !live && 'is-settled')} disabled={!live}>
      <legend className="n-ui-choices-question">{question}</legend>
      <div className="n-ui-choices-items" role="radiogroup" aria-label={question}>
        {options.map((option, index) => (
          <label key={option.key} className={cn('n-ui-choice', marked === index && 'is-selected')}>
            <input type="radio" name={name} checked={marked === index} onChange={() => setSelected(index)} />
            <span className="n-ui-choice-dot" aria-hidden="true" />
            <span className="n-ui-choice-label">{option.label}</span>
          </label>
        ))}
      </div>
      {open && (
        <button
          type="button"
          className="n-ui-choices-send"
          disabled={!live || selected === null}
          onClick={() => {
            const option = selected === null ? undefined : options[selected]
            if (!option || !pick) return
            pick(option.message)
          }}
        >
          <span>{action || t('chat.genui.choices-send')}</span>
          <ArrowRight aria-hidden="true" />
        </button>
      )}
    </fieldset>
  )
}

const str = (value: unknown) => (typeof value === 'string' ? value : '')
