import { Link, useNavigate, useSearch } from '@tanstack/react-router'
import { CircleAlert, KeyRound } from 'lucide-react'
import { useCallback, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { isAuthFieldError, type AuthErrorKey } from '@/features/auth'
import { useLogin, usePasskeyLogin } from '@/features/auth/use-auth'
import { useAuthOptions } from '@/hooks/use-auth-options'
import AuthField from './AuthField'
import AuthShell from './AuthShell'
import { BannedScreen, PendingScreen } from './AccountOverlay'
import { validateIdentifier, validatePassword } from './validation'

type Errors = Partial<Record<'identifier' | 'password', AuthErrorKey>>

export function LoginPage() {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const { redirect } = useSearch({ from: '/login' })
  const login = useLogin()
  const passkey = usePasskeyLogin()
  // The registration link is the administrator's to offer.
  const options = useAuthOptions()

  const [identifier, setIdentifier] = useState('')
  const [password, setPassword] = useState('')
  const [errors, setErrors] = useState<Errors>({})
  // The failure the user has since edited away — hidden, but the mutation
  // itself is never reset (resetting mid-flight would reopen the submit
  // path). A retry produces a fresh error object, which shows again.
  const [dismissedFailure, setDismissedFailure] = useState<unknown>(null)
  // The button's disabled state is styling; this ref is the actual guard
  // against a second request (requestSubmit, a fast second Enter, …).
  const submittingRef = useRef(false)

  const handleSubmit = async (event: React.FormEvent) => {
    event.preventDefault()
    if (submittingRef.current) return

    const next: Errors = {}
    const identifierError = validateIdentifier(identifier)
    const passwordError = validatePassword(password)
    if (identifierError) next.identifier = identifierError
    if (passwordError) next.password = passwordError

    setErrors(next)
    if (Object.keys(next).length > 0) return

    submittingRef.current = true
    try {
      await login.mutateAsync({ identifier, password })
      await navigate({ to: redirect ?? '/' })
    } catch {
      // Rendered from `login.error` below; mutateAsync only rethrows here.
    } finally {
      // Always released, so a failed attempt can be retried.
      submittingRef.current = false
    }
  }

  const failure = login.error && login.error !== dismissedFailure ? login.error : null
  const formError = isAuthFieldError(failure) ? failure.key : failure ? 'auth.error.unknown' : null
  // A passkey's failure is a line on the form; a pending or disabled account
  // says so there too (the overlays' "check again" re-sends a password).
  const passkeyFailure = passkey.error && passkey.error !== dismissedFailure ? passkey.error : null
  const passkeyError = isAuthFieldError(passkeyFailure) ? passkeyFailure.key : passkeyFailure ? 'auth.error.unknown' : null
  const busy = login.isPending || passkey.isPending

  const signInWithPasskey = async () => {
    if (submittingRef.current) return
    submittingRef.current = true
    if (login.error) setDismissedFailure(login.error)
    try {
      const user = await passkey.mutateAsync()
      if (user) await navigate({ to: redirect ?? '/' })
    } catch {
      // Rendered from `passkey.error` below.
    } finally {
      submittingRef.current = false
    }
  }

  const dismissFailure = () => {
    if (login.error) setDismissedFailure(login.error)
  }

  // An account that exists but cannot sign in - disabled, or waiting for an
  // administrator - gets the whole window rather than the form's banner.
  const banned = formError === 'auth.error.account-disabled'
  const pending = formError === 'auth.error.pending-approval'
  const contact = isAuthFieldError(failure) ? failure.admin : undefined
  const leaveOverlay = useCallback(() => {
    setDismissedFailure(login.error)
    setIdentifier('')
    setPassword('')
  }, [login.error])

  // Signs in again with what was just sent: the way in once the account has
  // been approved. Another refusal replaces login.error and the overlay
  // follows it (still pending, now disabled, or the form's banner).
  const checkAgain = async () => {
    if (!login.variables || submittingRef.current) return
    submittingRef.current = true
    try {
      await login.mutateAsync(login.variables)
      await navigate({ to: redirect ?? '/' })
    } catch {
      // Shown from login.error.
    } finally {
      submittingRef.current = false
    }
  }

  return (
    <AuthShell
      title={t('auth.login.title', { name: t('app.name') })}
      subtitle={t('auth.login.subtitle')}
      overlay={
        banned ? (
          <BannedScreen
            admin={contact}
            disabled={isAuthFieldError(failure) ? failure.disabled : undefined}
            onBack={leaveOverlay}
          />
        ) : pending ? (
          <PendingScreen admin={contact} check={checkAgain} onBack={leaveOverlay} />
        ) : null
      }
    >
      <form className="n-auth-form" onSubmit={(event) => void handleSubmit(event)} noValidate>
        <div className="n-auth-fields">
          <AuthField
            label={t('auth.field.identifier')}
            // `type="email"` would make the browser reject a username, and
            // autoComplete="username" is the token password managers use for
            // whichever handle signs you in — including an address.
            autoComplete="username"
            autoFocus
            placeholder={t('auth.field.identifier-placeholder')}
            value={identifier}
            disabled={busy}
            error={errors.identifier && t(errors.identifier)}
            onChange={(event) => {
              setIdentifier(event.target.value)
              setErrors((current) => ({ ...current, identifier: undefined }))
              dismissFailure()
            }}
          />

          <AuthField
            label={t('auth.field.password')}
            secret
            autoComplete="current-password"
            placeholder={t('auth.field.password-placeholder')}
            value={password}
            disabled={busy}
            error={errors.password && t(errors.password)}
            onChange={(event) => {
              setPassword(event.target.value)
              setErrors((current) => ({ ...current, password: undefined }))
              dismissFailure()
            }}
          />
        </div>

        <div className="n-auth-actions">
          {(passkeyError ?? (formError && !banned && !pending ? formError : null)) && (
            <div className="n-auth-alert" role="alert">
              <CircleAlert className="n-auth-alert-icon" aria-hidden="true" />
              <span>{t(passkeyError ?? formError!)}</span>
            </div>
          )}

          <button type="submit" className="n-auth-submit" disabled={busy}>
            <span>{t('auth.login.action')}</span>
            {login.isPending && (
              <span className="n-auth-spinner" data-motion="essential" aria-hidden="true" />
            )}
          </button>

          {options.passkeys && (
            <button type="button" className="n-auth-passkey" disabled={busy} onClick={() => void signInWithPasskey()}>
              {passkey.isPending ? (
                <span className="n-auth-spinner" data-motion="essential" aria-hidden="true" />
              ) : (
                <KeyRound aria-hidden="true" />
              )}
              <span>{t('auth.login.passkey')}</span>
            </button>
          )}

          {options.registration && (
            <div className="n-auth-switch">
              {t('auth.login.no-account')}{' '}
              <Link className="n-auth-link" to="/register">
                {t('auth.register.link')}
              </Link>
            </div>
          )}
        </div>
      </form>
    </AuthShell>
  )
}

export default LoginPage
