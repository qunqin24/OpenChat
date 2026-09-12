import { Link, useNavigate, useSearch } from '@tanstack/react-router'
import { AlertCircle } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { isAuthFieldError, type AuthErrorKey } from '@/features/auth'
import { useLogin } from '@/features/auth/use-auth'
import AuthField from './AuthField'
import AuthShell from './AuthShell'
import { validateEmail, validatePassword } from './validation'

type Errors = Partial<Record<'email' | 'password', AuthErrorKey>>

export function LoginPage() {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const { redirect } = useSearch({ from: '/login' })
  const login = useLogin()

  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  const [errors, setErrors] = useState<Errors>({})

  const handleSubmit = async (event: React.FormEvent) => {
    event.preventDefault()

    const next: Errors = {}
    const emailError = validateEmail(email)
    const passwordError = validatePassword(password)
    if (emailError) next.email = emailError
    if (passwordError) next.password = passwordError

    setErrors(next)
    if (Object.keys(next).length > 0) return

    try {
      await login.mutateAsync({ email, password })
      await navigate({ to: redirect ?? '/' })
    } catch {
      // Rendered from `login.error` below; mutateAsync only rethrows here.
    }
  }

  const failure = login.error
  const formError = isAuthFieldError(failure) ? failure.key : failure ? 'auth.error.unknown' : null

  return (
    <AuthShell
      title={t('auth.login.title')}
      subtitle={t('auth.login.subtitle')}
      footer={
        <>
          {t('auth.login.no-account')}{' '}
          <Link className="n-auth-link" to="/register">
            {t('auth.register.action')}
          </Link>
        </>
      }
    >
      <form className="n-auth-form" onSubmit={(event) => void handleSubmit(event)} noValidate>
        <AuthField
          label={t('auth.field.email')}
          type="email"
          autoComplete="email"
          autoFocus
          placeholder="you@example.com"
          value={email}
          disabled={login.isPending}
          error={errors.email && t(errors.email)}
          onChange={(event) => {
            setEmail(event.target.value)
            setErrors((current) => ({ ...current, email: undefined }))
          }}
        />

        <AuthField
          label={t('auth.field.password')}
          secret
          autoComplete="current-password"
          placeholder="••••••••"
          value={password}
          disabled={login.isPending}
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

        <button type="submit" className="n-auth-submit" disabled={login.isPending}>
          {login.isPending && <span className="n-auth-spinner" aria-hidden="true" />}
          <span>{t('auth.login.action')}</span>
        </button>
      </form>

    </AuthShell>
  )
}

export default LoginPage
