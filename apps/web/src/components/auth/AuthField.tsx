import { Eye, EyeOff } from 'lucide-react'
import { useId, useState, type ComponentProps } from 'react'
import { useTranslation } from 'react-i18next'

import { cn } from '@/lib/utils'

interface AuthFieldProps extends Omit<ComponentProps<'input'>, 'id' | 'className'> {
  label: string
  error?: string
  /** Adds the show/hide toggle and starts masked. */
  secret?: boolean
}

export function AuthField({ label, error, secret, ...props }: AuthFieldProps) {
  const { t } = useTranslation()
  const id = useId()
  const [revealed, setRevealed] = useState(false)

  return (
    <div className="n-auth-field">
      <label className="n-auth-label" htmlFor={id}>
        {label}
      </label>

      <div className={cn('n-auth-input-wrap', error && 'invalid')}>
        <input
          id={id}
          className="n-auth-input"
          type={secret ? (revealed ? 'text' : 'password') : props.type}
          aria-invalid={Boolean(error)}
          aria-describedby={error ? `${id}-error` : undefined}
          {...props}
        />

        {secret && (
          <button
            type="button"
            className="n-auth-reveal"
            aria-label={revealed ? t('auth.hide-password') : t('auth.show-password')}
            onClick={() => setRevealed((value) => !value)}
          >
            {revealed ? <EyeOff className="h-4 w-4" /> : <Eye className="h-4 w-4" />}
          </button>
        )}
      </div>

      {error && (
        <span className="n-auth-error" id={`${id}-error`} role="alert">
          {error}
        </span>
      )}
    </div>
  )
}

export default AuthField
