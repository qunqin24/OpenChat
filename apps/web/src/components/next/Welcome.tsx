import { Zap } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import ModelAvatar from './ModelAvatar'

/**
 * Suggestion ids. Kept as explicit keys rather than an i18n array so every
 * string stays statically referenced and type-safe across locales.
 */
const SUGGESTIONS = ['explain', 'code', 'polish', 'plan', 'compare', 'ideas'] as const

interface WelcomeProps {
  /** Fills the composer rather than sending, so the prompt can be edited. */
  onPick: (prompt: string) => void
  modelName: string
}

/** Empty-conversation state: the active model, a greeting, and starter prompts. */
export function Welcome({ onPick, modelName }: WelcomeProps) {
  const { t } = useTranslation()

  return (
    <div className="n-column n-welcome">
      <ModelAvatar name={modelName} size={44} />
      <h1 className="n-welcome-title">{modelName}</h1>
      <p className="n-welcome-sub">{t('next.welcome.greeting')}</p>

      <div className="n-suggest-head">
        <Zap className="h-3.5 w-3.5" />
        {t('next.welcome.suggestions')}
      </div>

      <div className="n-suggest-grid">
        {SUGGESTIONS.map((id) => (
          <button
            key={id}
            type="button"
            className="n-suggest"
            onClick={() => {
              onPick(t(`next.welcome.items.${id}.prompt`))
              document.getElementById('input')?.focus()
            }}
          >
            <span className="n-suggest-title">{t(`next.welcome.items.${id}.title`)}</span>
            <span className="n-suggest-detail">{t(`next.welcome.items.${id}.detail`)}</span>
          </button>
        ))}
      </div>
    </div>
  )
}

export default Welcome
