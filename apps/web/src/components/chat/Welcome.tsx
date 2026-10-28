import { Ghost } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { useSession } from '@/features/auth/use-auth'
import { dayPeriod } from '@/lib/greeting'
import { useChatStore } from '@/stores/chat-store'
import { TemporaryNote } from './TemporaryChat'

/**
 * Suggestion ids. Kept as explicit keys rather than an i18n array so every
 * string stays statically referenced and type-safe across locales.
 */
const SUGGESTIONS = ['explain', 'code', 'polish', 'plan', 'compare', 'ideas'] as const

/**
 * A new chat's greeting, as Claude's: our mark beside a line for the time of
 * day with the person's name, in a serif face. On a wide screen the page puts
 * it and the composer together in the middle (`.n-main.is-welcome`), the
 * starters under the composer (WelcomeStarters). A temporary chat says what
 * it is instead.
 */
export function Welcome() {
  const { t } = useTranslation()
  const temporary = useChatStore((s) => s.temporaryChat)
  const name = useSession().data?.name ?? ''
  // The hour the page was opened at: a greeting that changed under the
  // person's eyes would be odd.
  const [period] = useState(() => dayPeriod(new Date().getHours()))

  return (
    <div className="n-welcome n-chat-fade">
      {temporary ? (
        <div className="n-welcome-heading">
          <h1 className="n-welcome-title">
            <Ghost className="n-welcome-ghost" aria-hidden="true" />
            {t('chat.temporary.title')}
          </h1>
          <TemporaryNote />
        </div>
      ) : (
        <h1 className="n-welcome-greeting">
          <span className="n-welcome-logo" aria-hidden="true" />
          <span>{name ? t(`chat.welcome.hello.${period}`, { name }) : t(`chat.welcome.hello-bare.${period}`)}</span>
        </h1>
      )}
    </div>
  )
}

/**
 * Starters under the composer of a new chat: a row of quiet pills that fill the
 * composer, to be edited, rather than send.
 */
export function WelcomeStarters({ onPick }: { onPick: (prompt: string) => void }) {
  const { t } = useTranslation()
  return (
    <div className="n-welcome-starters" role="group" aria-label={t('chat.welcome.suggestions')}>
      {SUGGESTIONS.map((id, index) => (
        <button
          key={id}
          type="button"
          className="n-starter"
          style={{ animationDelay: `${index * 40}ms` }}
          title={t(`chat.welcome.items.${id}.detail`)}
          onClick={() => {
            onPick(t(`chat.welcome.items.${id}.prompt`))
            document.getElementById('input')?.focus()
          }}
        >
          {t(`chat.welcome.items.${id}.title`)}
        </button>
      ))}
    </div>
  )
}

export default Welcome
