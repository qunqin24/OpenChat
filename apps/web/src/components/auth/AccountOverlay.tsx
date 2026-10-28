import { Ban } from 'lucide-react'
import { useEffect, useRef, useState, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'

import { IconMark } from '@/assets/custom/icon-mark'
import type { AdminContact } from '@/features/auth'
import { WaitRipples } from './Ripples'

/**
 * The whole window, for an account that exists but cannot sign in: waiting
 * for approval, or disabled. Its own page rather than a dialog over the form
 * (the flow is after OpenWebUI's AccountPending overlay): the brand in the
 * corner, one centred column with a badge, a title with its subtitle, the
 * body, the administrator to contact, and the way on. Waiting shows the mark
 * with its rings spreading across the window; a refusal is a still red badge.
 *
 * The server says `pending_approval` or `account_disabled` only after the
 * password checks out, so neither tells anything to someone guessing.
 */
function AccountOverlay({
  tone,
  title,
  subtitle,
  text,
  admin,
  status,
  action,
  secondary,
  onEscape,
}: {
  tone: 'wait' | 'refused'
  title: string
  subtitle: string
  text: string
  admin?: AdminContact
  /** A line under the body, e.g. the outcome of checking again. */
  status?: ReactNode
  action: { label: string; onClick: () => void; busy?: boolean }
  secondary?: { label: string; onClick: () => void }
  onEscape: () => void
}) {
  const { t } = useTranslation()
  const button = useRef<HTMLButtonElement>(null)

  useEffect(() => {
    button.current?.focus({ preventScroll: true })
  }, [])

  useEffect(() => {
    const onKey = (event: KeyboardEvent) => {
      if (event.key === 'Escape') onEscape()
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [onEscape])

  return (
    <div
      className="n-account-overlay"
      data-tone={tone}
      role="alertdialog"
      aria-modal="true"
      aria-labelledby="n-account-overlay-title"
      aria-describedby="n-account-overlay-text"
    >
      <div className="n-auth-brand n-account-overlay-brand" aria-hidden="true">
        <IconMark className="n-auth-brand-mark" />
        {t('app.name')}
      </div>
      <div className="n-account-overlay-column">
        <div className="n-account-overlay-badge" aria-hidden="true">
          {tone === 'wait' ? (
            <>
              <WaitRipples />
              <IconMark className="n-account-overlay-mark" />
            </>
          ) : (
            <Ban className="n-account-overlay-ban" />
          )}
        </div>
        <h1 id="n-account-overlay-title" className="n-account-overlay-title">
          {title}
          <span className="n-account-overlay-subtitle">{subtitle}</span>
        </h1>
        <p id="n-account-overlay-text" className="n-account-overlay-text">
          {text}
        </p>
        {admin && <AdminCard admin={admin} />}
        {status && (
          <p className="n-account-overlay-status" role="status">
            {status}
          </p>
        )}
        <div className="n-account-overlay-actions">
          <button
            ref={button}
            type="button"
            className="n-auth-submit"
            disabled={action.busy}
            onClick={action.onClick}
          >
            <span>{action.label}</span>
            {action.busy && <span className="n-auth-spinner" data-motion="essential" aria-hidden="true" />}
          </button>
          {secondary && (
            <button type="button" className="n-account-overlay-link" onClick={secondary.onClick}>
              {secondary.label}
            </button>
          )}
        </div>
      </div>
    </div>
  )
}

/**
 * Whom to ask: the administrator's initial, name and address (a mailto link).
 * The server sends it only to someone who proved the password of the account
 * (or just registered it).
 */
function AdminCard({ admin }: { admin: AdminContact }) {
  const { t } = useTranslation()
  const initial = Array.from(admin.name.trim())[0]?.toUpperCase() ?? '?'
  return (
    <div className="n-account-overlay-admin">
      <span className="n-account-overlay-avatar" aria-hidden="true">
        {initial}
      </span>
      <span className="n-account-overlay-who">
        <span className="n-account-overlay-name">{t('auth.overlay.admin', { name: admin.name })}</span>
        <a className="n-account-overlay-mail" href={`mailto:${admin.email}`}>
          {admin.email}
        </a>
      </span>
    </div>
  )
}

/** Signing in to a disabled account. "Back" returns to an empty form. */
export function BannedScreen({ admin, onBack }: { admin?: AdminContact; onBack: () => void }) {
  const { t } = useTranslation()
  return (
    <AccountOverlay
      tone="refused"
      title={t('auth.banned.title')}
      subtitle={t('auth.banned.subtitle')}
      text={t('auth.banned.text')}
      admin={admin}
      action={{ label: t('auth.banned.back'), onClick: onBack }}
      onEscape={onBack}
    />
  )
}

/**
 * An account waiting for an administrator: after registering in approval mode,
 * or signing in before approval. "Check again" signs in with the credentials
 * just used - if the account has been approved meanwhile, that is the way in;
 * if not, the screen stays and says so. `check` resolves whether or not it got
 * in; the caller moves on (or swaps to the banned screen) itself.
 */
export function PendingScreen({
  admin,
  check,
  onBack,
}: {
  admin?: AdminContact
  check: () => Promise<void>
  onBack: () => void
}) {
  const { t } = useTranslation()
  const [checking, setChecking] = useState(false)
  const [checked, setChecked] = useState(false)

  const checkAgain = async () => {
    setChecking(true)
    try {
      await check()
    } finally {
      setChecking(false)
      setChecked(true)
    }
  }

  return (
    <AccountOverlay
      tone="wait"
      title={t('auth.pending.title')}
      subtitle={t('auth.pending.subtitle')}
      text={t('auth.pending.text')}
      admin={admin}
      status={checked && !checking ? t('auth.pending.still') : undefined}
      action={{ label: t('auth.pending.check'), onClick: () => void checkAgain(), busy: checking }}
      secondary={{ label: t('auth.pending.back'), onClick: onBack }}
      onEscape={onBack}
    />
  )
}
