import { useRouter } from '@tanstack/react-router'
import { useEffect, useState, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'

import { IconMark } from '@/assets/custom/icon-mark'
import { warmSignedInApp } from '@/lib/warm-app'
import { PanelRipples } from './Ripples'

interface AuthShellProps {
  title: string
  /** One quiet line under the title. */
  subtitle?: string
  /** Shown above the title, in a soft disc (e.g. the lock on "registration closed"). */
  icon?: ReactNode
  /** A full-window page over everything (AccountOverlay). Rendered outside the
      form column, so nothing styled there (a transform, say) can trap its
      `position: fixed`. */
  overlay?: ReactNode
  children: ReactNode
}

/** Set once the entrance has played: it is for arriving, not for going from
    sign-in to sign-up and back, which should just show the other form. */
let entrancePlayed = false

/**
 * Shared frame for the sign-in and sign-up screens: a dark panel on the left
 * carrying the brand — the mark's rings drawn across it, and one line about
 * what OpenChat is for — and the form on the right. On a phone the panel
 * becomes a band across the top and the form slides up over its lower edge.
 */
export function AuthShell({ title, subtitle, icon, overlay, children }: AuthShellProps) {
  const { t } = useTranslation()
  const router = useRouter()
  const [instant] = useState(() => entrancePlayed)
  useEffect(() => {
    entrancePlayed = true
  }, [])
  // While the form waits, the chat it leads to is downloaded (lib/warm-app.ts).
  useEffect(() => warmSignedInApp(router), [router])

  return (
    <div className="n-auth n-auth-split" data-instant={instant || undefined}>
      <aside className="n-auth-panel" aria-hidden="true">
        <PanelRipples />
        <div className="n-auth-brand">
          <IconMark className="n-auth-brand-mark" />
          {t('app.name')}
        </div>
        <div className="n-auth-claim">
          <p className="n-auth-claim-line">{t('auth.panel.claim')}</p>
          <p className="n-auth-claim-tagline">{t('app.tagline')}</p>
        </div>
      </aside>

      <main className="n-auth-side">
        <div className="n-auth-body">
          <div className="n-auth-head">
            {icon && <div className="n-auth-head-icon">{icon}</div>}
            <h1 className="n-auth-title">{title}</h1>
            {subtitle && <p className="n-auth-subtitle">{subtitle}</p>}
          </div>
          {children}
        </div>
      </main>
      {overlay}
    </div>
  )
}

export default AuthShell
