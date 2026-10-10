import { Link, useNavigate, useSearch } from '@tanstack/react-router'
import { useCallback, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { isAuthFieldError, type AuthErrorKey } from '@/features/auth'
import { useLogin } from '@/features/auth/use-auth'
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
  // The registration link and the guest button are the administrator's to offer.
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
    <AuthShell title={t('auth.login.title', { name: t('app.name') })}>
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
            disabled={login.isPending}
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
            disabled={login.isPending}
            error={errors.password && t(errors.password)}
            onChange={(event) => {
              setPassword(event.target.value)
              setErrors((current) => ({ ...current, password: undefined }))
              dismissFailure()
            }}
          />
        </div>

        <div className="n-auth-actions">
          {formError && !banned && !pending && (
            <div className="n-auth-alert" role="alert">
              {t(formError)}
            </div>
          )}

          <button type="submit" className="n-auth-submit" disabled={login.isPending}>
            <span>{t('auth.login.action')}</span>
            {login.isPending && (
              <span className="n-auth-spinner" data-motion="essential" aria-hidden="true" />
            )}
          </button>

          {options.registration && (
            <div className="n-auth-switch">
              {t('auth.login.no-account')}{' '}
              <Link className="n-auth-link" to="/register">
                {t('auth.register.link')}
              </Link>
            </div>
          )}

          {options.guest && (
            <div className="n-auth-guest">
              <Link className="n-auth-guest-button" to="/guest">
                {t('auth.login.guest')}
              </Link>
              <p className="n-auth-guest-hint">{t('auth.login.guest-hint')}</p>
            </div>
          )}
        </div>
      </form>
      {banned && <BannedScreen admin={contact} onBack={leaveOverlay} />}
      {pending && <PendingScreen admin={contact} check={checkAgain} onBack={leaveOverlay} />}
    </AuthShell>
  )
}

export default LoginPage
