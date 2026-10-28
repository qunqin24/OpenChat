import { CircleAlert, Eye, EyeOff } from 'lucide-react'
import { useId, useState, type ComponentProps } from 'react'
import { useTranslation } from 'react-i18next'

interface AuthFieldProps extends Omit<ComponentProps<'input'>, 'id' | 'className'> {
  label: string
  /** A quiet note at the right end of the label row, e.g. the minimum length. */
  hint?: string
  error?: string
  /** Adds the show/hide toggle and starts masked. */
  secret?: boolean
}

/**
 * One label + input pair in the auth form (with OpenWebUI's SensitiveInput
 * toggle for `secret`). The input sits in a filled control that carries the
 * hover, focus and error states, so the eye button lives inside the box.
 */
export function AuthField({ label, hint, error, secret, ...props }: AuthFieldProps) {
  const { t } = useTranslation()
  const id = useId()
  const [revealed, setRevealed] = useState(false)

  return (
    <div className="n-auth-field">
      <div className="n-auth-label-row">
        <label className="n-auth-label" htmlFor={id}>
          {label}
        </label>
        {hint && <span className="n-auth-hint">{hint}</span>}
      </div>

      <div className="n-auth-control" data-invalid={error ? true : undefined}>
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
            {revealed ? (
              <EyeOff className="h-4 w-4" aria-hidden="true" />
            ) : (
              <Eye className="h-4 w-4" aria-hidden="true" />
            )}
          </button>
        )}
      </div>

      {error && (
        <span className="n-auth-error" id={`${id}-error`} role="alert">
          <CircleAlert className="n-auth-error-icon" aria-hidden="true" />
          {error}
        </span>
      )}
    </div>
  )
}

export default AuthField
