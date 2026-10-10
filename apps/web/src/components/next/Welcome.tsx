import type { ChatModel } from '@openchat/api-client'
import { Zap } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { ModelIcon } from '@/components/model-icon'

/**
 * Suggestion ids. Kept as explicit keys rather than an i18n array so every
 * string stays statically referenced and type-safe across locales.
 */
const SUGGESTIONS = ['explain', 'code', 'polish', 'plan', 'compare', 'ideas'] as const

interface WelcomeProps {
  /** Fills the composer rather than sending, so the prompt can be edited. */
  onPick: (prompt: string) => void
  modelName: string
  /** The selected model's catalogue entry; absent when no model is offered. */
  model?: ChatModel
}

/**
 * OpenWebUI's chat landing page (ChatPlaceholder at 0a7c15832f): the avatar
 * owns the first line, the model name and grey greeting follow it, then a
 * two-column starter grid. The composer is docked outside this component, so
 * the welcome never has to know about it.
 */
export function Welcome({ onPick, modelName, model }: WelcomeProps) {
  const { t } = useTranslation()

  return (
    <div className="n-welcome n-chat-fade">
      {/* The model's own mark when there is one; with nothing configured, the
          heading reads "OpenChat" and the mark is ours. */}
      <span className="n-welcome-avatar" aria-hidden="true">
        {model ? (
          <ModelIcon
            subject="model"
            className="n-welcome-mark rounded-md"
            providerId={model.providerId || undefined}
            family={model.family || undefined}
            name={model.name}
          />
        ) : (
          <span className="n-welcome-logo" />
        )}
      </span>

      <div className="n-welcome-heading">
        <h1 className="n-welcome-title" title={modelName}>
          {modelName}
        </h1>
        <p className="n-welcome-sub">{t('next.welcome.greeting')}</p>
      </div>

      <div className="n-welcome-suggestions">
        <div className="n-suggest-head">
          <Zap className="h-3.5 w-3.5" aria-hidden="true" />
          {t('next.welcome.suggestions')}
        </div>
        <div className="n-suggest-viewport">
          <div
            className="n-suggest-grid"
            role="group"
            aria-label={t('next.welcome.suggestions')}
          >
            {SUGGESTIONS.map((id, index) => (
              <button
                key={id}
                type="button"
                className="n-suggest"
                style={{ animationDelay: `${index * 45}ms` }}
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
      </div>
    </div>
  )
}

export default Welcome
