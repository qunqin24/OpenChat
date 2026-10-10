import { Eye, EyeOff } from 'lucide-react'
import { useId, useState, type ComponentProps } from 'react'
import { useTranslation } from 'react-i18next'

interface AuthFieldProps extends Omit<ComponentProps<'input'>, 'id' | 'className'> {
  label: string
  error?: string
  /** Adds the show/hide toggle and starts masked. */
  secret?: boolean
}

/**
 * One label + input pair in the auth form, after OpenWebUI's fields (and its
 * SensitiveInput for `secret`): the input is bare text on the page — no box —
 * so the only error affordance is the message beneath it, not a ring.
 */
export function AuthField({ label, error, secret, ...props }: AuthFieldProps) {
  const { t } = useTranslation()
  const id = useId()
  const [revealed, setRevealed] = useState(false)

  const input = (
    <input
      id={id}
      className="n-auth-input"
      type={secret ? (revealed ? 'text' : 'password') : props.type}
      aria-invalid={Boolean(error)}
      aria-describedby={error ? `${id}-error` : undefined}
      {...props}
    />
  )

  return (
    <div className="n-auth-field">
      <label className="n-auth-label" htmlFor={id}>
        {label}
      </label>

      {secret ? (
        <div className="n-auth-secret">
          {input}
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
        </div>
      ) : (
        input
      )}

      {error && (
        <span className="n-auth-error" id={`${id}-error`} role="alert">
          {error}
        </span>
      )}
    </div>
  )
}

export default AuthField
