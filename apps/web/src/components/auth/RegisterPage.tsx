import { Link, useNavigate } from '@tanstack/react-router'
import { useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { isAuthFieldError, type AuthErrorKey } from '@/features/auth'
import { useRegister } from '@/features/auth/use-auth'
import { useAuthOptions } from '@/hooks/use-auth-options'
import AuthField from './AuthField'
import AuthShell from './AuthShell'
import { validateEmail, validateName, validatePassword } from './validation'

type Errors = Partial<Record<'email' | 'name' | 'password', AuthErrorKey>>
type FormField = 'email' | 'name' | 'password'

/** Server-side failures that blame one of this form's fields. `toAuthError`
    defaults a fieldless failure to 'password', so the key is checked too —
    'unknown' belongs in the banner, not under an innocent input. */
function formFieldOf(failure: unknown): FormField | null {
  if (!isAuthFieldError(failure) || failure.key === 'auth.error.unknown') return null
  return failure.field === 'name' || failure.field === 'email' || failure.field === 'password'
    ? failure.field
    : null
}

export function RegisterPage() {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const register = useRegister()
  const options = useAuthOptions()
  // Registered, but an administrator has to approve the account before it can
  // sign in: there is no session, so the page says so instead of moving on.
  const [waiting, setWaiting] = useState(false)

  const [name, setName] = useState('')
  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  const [errors, setErrors] = useState<Errors>({})
  // Server failures the user has since edited away — hidden per field (or as
  // a banner), but the mutation is never reset, so a retry's fresh error
  // object still shows.
  const [dismissedField, setDismissedField] = useState<{ failure: unknown; field: FormField } | null>(
    null,
  )
  const [dismissedFormFailure, setDismissedFormFailure] = useState<unknown>(null)
  // The button's disabled state is styling; this ref is the actual guard
  // against a second request (requestSubmit, a fast second Enter, …).
  const submittingRef = useRef(false)

  const handleSubmit = async (event: React.FormEvent) => {
    event.preventDefault()
    if (submittingRef.current) return

    const next: Errors = {}
    const nameError = validateName(name)
    const emailError = validateEmail(email)
    const passwordError = validatePassword(password, { signup: true })
    if (nameError) next.name = nameError
    if (emailError) next.email = emailError
    if (passwordError) next.password = passwordError

    setErrors(next)
    if (Object.keys(next).length > 0) return

    submittingRef.current = true
    try {
      const outcome = await register.mutateAsync({ email, name, password })
      if (outcome.status === 'pending') {
        setWaiting(true)
        return
      }
      await navigate({ to: '/' })
    } catch {
      // Rendered from `register.error` below.
    } finally {
      // Always released, so a failed attempt can be retried.
      submittingRef.current = false
    }
  }

  const failure = register.error
  const failureField = formFieldOf(failure)
  const formError =
    failure && !failureField && failure !== dismissedFormFailure
      ? isAuthFieldError(failure)
        ? failure.key
        : 'auth.error.unknown'
      : null

  /** A server field error stays next to its input until that input is edited. */
  const serverError = (field: FormField): string | undefined => {
    if (!failure || failureField !== field) return undefined
    if (dismissedField && dismissedField.failure === failure && dismissedField.field === field) {
      return undefined
    }
    return isAuthFieldError(failure) ? t(failure.key) : undefined
  }

  const dismissFailure = (field: FormField) => {
    if (failure && failureField === field) setDismissedField({ failure, field })
    if (failure && !failureField) setDismissedFormFailure(failure)
  }

  if (waiting) {
    return (
      <AuthShell title={t('auth.register.pending-title')}>
        <p className="n-auth-notice" role="status">
          {t('auth.register.pending')}
        </p>
        <div className="n-auth-switch">
          <Link className="n-auth-link" to="/login">
            {t('auth.register.to-login')}
          </Link>
        </div>
      </AuthShell>
    )
  }

  if (!options.registration) {
    return (
      <AuthShell title={t('auth.register.closed-title')}>
        <p className="n-auth-notice" role="status">
          {t('auth.register.closed')}
        </p>
        <div className="n-auth-switch">
          <Link className="n-auth-link" to="/login">
            {t('auth.register.to-login')}
          </Link>
        </div>
      </AuthShell>
    )
  }

  return (
    <AuthShell title={t('auth.register.title', { name: t('app.name') })}>
      <form className="n-auth-form" onSubmit={(event) => void handleSubmit(event)} noValidate>
        <div className="n-auth-fields">
          <AuthField
            label={t('auth.field.name')}
            autoComplete="name"
            autoFocus
            placeholder={t('auth.field.name-placeholder')}
            value={name}
            disabled={register.isPending}
            error={(errors.name && t(errors.name)) ?? serverError('name')}
            onChange={(event) => {
              setName(event.target.value)
              setErrors((current) => ({ ...current, name: undefined }))
              dismissFailure('name')
            }}
          />

          <AuthField
            label={t('auth.field.email')}
            type="email"
            autoComplete="email"
            placeholder={t('auth.field.email-placeholder')}
            value={email}
            disabled={register.isPending}
            error={(errors.email && t(errors.email)) ?? serverError('email')}
            onChange={(event) => {
              setEmail(event.target.value)
              setErrors((current) => ({ ...current, email: undefined }))
              dismissFailure('email')
            }}
          />

          <AuthField
            label={t('auth.field.password')}
            secret
            autoComplete="new-password"
            placeholder={t('auth.field.password-placeholder')}
            value={password}
            disabled={register.isPending}
            error={(errors.password && t(errors.password)) ?? serverError('password')}
            onChange={(event) => {
              setPassword(event.target.value)
              setErrors((current) => ({ ...current, password: undefined }))
              dismissFailure('password')
            }}
          />
        </div>

        <div className="n-auth-actions">
          {formError && (
            <div className="n-auth-alert" role="alert">
              {t(formError)}
            </div>
          )}

          <button type="submit" className="n-auth-submit" disabled={register.isPending}>
            <span>{t('auth.register.action')}</span>
            {register.isPending && (
              <span className="n-auth-spinner" data-motion="essential" aria-hidden="true" />
            )}
          </button>

          <div className="n-auth-switch">
            {t('auth.register.have-account')}{' '}
            <Link className="n-auth-link" to="/login">
              {t('auth.login.action')}
            </Link>
          </div>
        </div>
      </form>
    </AuthShell>
  )
}

export default RegisterPage
