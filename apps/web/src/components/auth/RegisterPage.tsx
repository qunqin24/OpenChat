import { Link, useNavigate } from '@tanstack/react-router'
import { AlertCircle } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { isAuthFieldError, type AuthErrorKey } from '@/features/auth'
import { useRegister } from '@/features/auth/use-auth'
import AuthField from './AuthField'
import AuthShell from './AuthShell'
import { MIN_PASSWORD_LENGTH, validateEmail, validateName, validatePassword } from './validation'

type Errors = Partial<Record<'email' | 'name' | 'password', AuthErrorKey>>

export function RegisterPage() {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const register = useRegister()

  const [name, setName] = useState('')
  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  const [errors, setErrors] = useState<Errors>({})

  const handleSubmit = async (event: React.FormEvent) => {
    event.preventDefault()

    const next: Errors = {}
    const nameError = validateName(name)
    const emailError = validateEmail(email)
    const passwordError = validatePassword(password, { signup: true })
    if (nameError) next.name = nameError
    if (emailError) next.email = emailError
    if (passwordError) next.password = passwordError

    setErrors(next)
    if (Object.keys(next).length > 0) return

    try {
      await register.mutateAsync({ email, name, password })
      await navigate({ to: '/' })
    } catch {
      // Rendered from `register.error` below.
    }
  }

  const failure = register.error
  // A taken address belongs next to the email input, not in the form-level
  // banner, so it is routed to the field and kept out of `formError`.
  const fieldFailure = isAuthFieldError(failure) ? failure : null
  const formError = failure && !fieldFailure ? 'auth.error.unknown' : null

  return (
    <AuthShell
      title={t('auth.register.title')}
      subtitle={t('auth.register.subtitle')}
      footer={
        <>
          {t('auth.register.have-account')}{' '}
          <Link className="n-auth-link" to="/login">
            {t('auth.login.action')}
          </Link>
        </>
      }
    >
      <form className="n-auth-form" onSubmit={(event) => void handleSubmit(event)} noValidate>
        <AuthField
          label={t('auth.field.name')}
          autoComplete="name"
          autoFocus
          placeholder={t('auth.field.name-placeholder')}
          value={name}
          disabled={register.isPending}
          error={errors.name && t(errors.name)}
          onChange={(event) => {
            setName(event.target.value)
            setErrors((current) => ({ ...current, name: undefined }))
          }}
        />

        <AuthField
          label={t('auth.field.email')}
          type="email"
          autoComplete="email"
          placeholder="you@example.com"
          value={email}
          disabled={register.isPending}
          error={
            (errors.email && t(errors.email)) ??
            (fieldFailure?.field === 'email' ? t(fieldFailure.key) : undefined)
          }
          onChange={(event) => {
            setEmail(event.target.value)
            setErrors((current) => ({ ...current, email: undefined }))
          }}
        />

        <AuthField
          label={t('auth.field.password')}
          secret
          autoComplete="new-password"
          placeholder={t('auth.field.password-placeholder', { n: MIN_PASSWORD_LENGTH })}
          value={password}
          disabled={register.isPending}
          error={errors.password && t(errors.password)}
          onChange={(event) => {
            setPassword(event.target.value)
            setErrors((current) => ({ ...current, password: undefined }))
          }}
        />

        {formError && (
          <div className="n-auth-alert" role="alert">
            <AlertCircle className="mt-0.5 h-4 w-4 shrink-0" />
            <span>{t(formError)}</span>
          </div>
        )}

        <button type="submit" className="n-auth-submit" disabled={register.isPending}>
          {register.isPending && <span className="n-auth-spinner" aria-hidden="true" />}
          <span>{t('auth.register.action')}</span>
        </button>
      </form>
    </AuthShell>
  )
}

export default RegisterPage
