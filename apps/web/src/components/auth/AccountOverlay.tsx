import { Ban } from 'lucide-react'
import { useEffect, useRef, useState, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'

import { IconMark } from '@/assets/custom/icon-mark'
import type { AdminContact } from '@/features/auth'
import { categoryKey, type AccountDisabled } from '@openchat/chat-core/account-disabled'
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
  children,
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
  /** Between the body and the administrator: why, for a disabled account. */
  children?: ReactNode
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
        {children}
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

/**
 * Why an account is disabled: what the administrator wrote (or that they wrote
 * nothing), or for a moderation ban the categories its messages were blocked
 * for; and since when.
 */
function DisabledReason({ details }: { details: AccountDisabled }) {
  const { t, i18n } = useTranslation()
  const moderation = details.reason === 'moderation'
  const categories = (details.categories ?? []).map((category) =>
    t(`admin.moderation.category.${categoryKey(category)}` as 'admin.moderation.category.sexual', { defaultValue: category }),
  )
  const reason = moderation
    ? categories.length > 0
      ? t('auth.disabled.moderation-reason-categories', {
          categories: new Intl.ListFormat(i18n.language, { style: 'narrow', type: 'conjunction' }).format(categories),
        })
      : t('auth.disabled.moderation-reason')
    : details.note
      ? t('auth.disabled.note', { note: details.note })
      : t('auth.disabled.no-note')
  const at = details.disabledAt ? new Date(details.disabledAt) : null
  return (
    <dl className="n-account-overlay-reason">
      <dt>{t('auth.disabled.reason')}</dt>
      <dd>{reason}</dd>
      {at && !Number.isNaN(at.getTime()) && (
        <>
          <dt>{t(moderation ? 'auth.disabled.moderation-at' : 'auth.disabled.admin-at')}</dt>
          <dd>
            <time dateTime={details.disabledAt}>
              {at.toLocaleString(i18n.language, { dateStyle: 'long', timeStyle: 'short' })}
            </time>
          </dd>
        </>
      )}
    </dl>
  )
}

/** The title, subtitle and body for a disabled account, by who disabled it. */
function useDisabledWords(details: AccountDisabled | undefined) {
  const { t } = useTranslation()
  if (!details) return { title: t('auth.banned.title'), subtitle: t('auth.banned.subtitle'), text: t('auth.banned.text') }
  if (details.reason === 'moderation') {
    return {
      title: t('auth.disabled.moderation-title'),
      subtitle: t('auth.disabled.moderation-subtitle'),
      text:
        details.strikes && details.windowHours
          ? t('auth.disabled.moderation-text', { count: details.strikes, hours: details.windowHours })
          : t('auth.disabled.moderation-text-bare'),
    }
  }
  return { title: t('auth.disabled.admin-title'), subtitle: t('auth.disabled.admin-subtitle'), text: t('auth.disabled.admin-text') }
}

/** Signing in to a disabled account. "Back" returns to an empty form. */
export function BannedScreen({
  admin,
  disabled,
  onBack,
}: {
  admin?: AdminContact
  disabled?: AccountDisabled
  onBack: () => void
}) {
  const { t } = useTranslation()
  const words = useDisabledWords(disabled)
  return (
    <AccountOverlay
      tone="refused"
      {...words}
      admin={admin}
      action={{ label: t('auth.banned.back'), onClick: onBack }}
      onEscape={onBack}
    >
      {disabled && <DisabledReason details={disabled} />}
    </AccountOverlay>
  )
}

/**
 * A signed-in account that has been disabled (the /disabled page): the session
 * is kept, so this says why in place of the app. Signing out ends it; "check
 * again" goes back in once the account is enabled, or says it still is not.
 */
export function DisabledScreen({
  details,
  checking,
  still,
  onCheck,
  onSignOut,
}: {
  details: AccountDisabled
  checking: boolean
  still: boolean
  onCheck: () => void
  onSignOut: () => void
}) {
  const { t } = useTranslation()
  const words = useDisabledWords(details)
  return (
    <AccountOverlay
      tone="refused"
      {...words}
      admin={details.admin}
      status={still && !checking ? t('auth.disabled.still') : undefined}
      action={{ label: t('auth.disabled.sign-out'), onClick: onSignOut }}
      secondary={{ label: checking ? t('auth.disabled.checking') : t('auth.disabled.check'), onClick: onCheck }}
      onEscape={() => undefined}
    >
      <DisabledReason details={details} />
    </AccountOverlay>
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
