import { useEffect, useRef, useState, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'

import type { AdminContact } from '@/features/auth'

/**
 * A frosted cover over the whole window for an account that exists but cannot
 * sign in: waiting for approval, or disabled. A port of OpenWebUI's
 * pending-account overlay (src/lib/components/layout/Overlay/AccountPending.svelte):
 * the page blurred behind, a centred `max-w-md` column, a two-line
 * `text-2xl font-normal` title, `text-sm` body, the administrator to contact,
 * one white pill button and a small underlined link under it.
 *
 * The server says `pending_approval` or `account_disabled` only after the
 * password checks out, so neither tells anything to someone guessing.
 */
function AccountOverlay({
  title,
  subtitle,
  text,
  admin,
  status,
  action,
  secondary,
  onEscape,
}: {
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
      role="alertdialog"
      aria-modal="true"
      aria-labelledby="n-account-overlay-title"
      aria-describedby="n-account-overlay-text"
    >
      <div className="n-account-overlay-column">
        <h1 id="n-account-overlay-title" className="n-account-overlay-title">
          {title}
          <br />
          {subtitle}
        </h1>
        <p id="n-account-overlay-text" className="n-account-overlay-text">
          {text}
        </p>
        {admin && <AdminLine admin={admin} />}
        {status && (
          <p className="n-account-overlay-status" role="status">
            {status}
          </p>
        )}
        <div className="n-account-overlay-actions">
          <button
            ref={button}
            type="button"
            className="n-account-overlay-button"
            disabled={action.busy}
            onClick={action.onClick}
          >
            {action.label}
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
 * `mt-4 text-sm font-normal text-center`: "Admin: name (email)", the email a
 * mailto link. The server sends it only to someone who proved the password of
 * the account (or just registered it).
 */
function AdminLine({ admin }: { admin: AdminContact }) {
  const { t } = useTranslation()
  return (
    <p className="n-account-overlay-admin">
      {t('auth.overlay.admin', { name: admin.name })} (
      <a className="n-account-overlay-mail" href={`mailto:${admin.email}`}>
        {admin.email}
      </a>
      )
    </p>
  )
}

/** Signing in to a disabled account. "Back" returns to an empty form. */
export function BannedScreen({ admin, onBack }: { admin?: AdminContact; onBack: () => void }) {
  const { t } = useTranslation()
  return (
    <AccountOverlay
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
 * A guest whose network address is banned from guest mode (content moderation):
 * the same frosted cover as a disabled account's, over the whole window. The
 * one way on is to sign in, if they have an account; the page behind asks the
 * server for nothing more.
 */
export function GuestBannedScreen({ until, onSignIn }: { until: string | null; onSignIn: () => void }) {
  const { t, i18n } = useTranslation()
  const when = until ? new Date(until) : null
  return (
    <AccountOverlay
      title={t('guest.banned.title')}
      subtitle={t('guest.banned.subtitle')}
      text={t('guest.banned.text')}
      status={
        when && !Number.isNaN(when.getTime())
          ? t('guest.banned.until', { time: when.toLocaleString(i18n.language) })
          : t('guest.banned.permanent')
      }
      action={{ label: t('guest.banned.sign-in'), onClick: onSignIn }}
      onEscape={() => undefined}
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
