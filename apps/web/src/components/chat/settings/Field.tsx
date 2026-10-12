import { useId, type ComponentProps } from 'react'

interface FieldProps extends Omit<ComponentProps<'input'>, 'id' | 'className' | 'onChange'> {
  label: string
  hint?: string
  error?: string
  onChange: (value: string) => void
}

/**
 * A labelled field of the settings dialog, in the classic style: a small bold
 * label over text that is only text — no box — with its hint and error under it.
 */
export function Field({ label, hint, error, onChange, ...props }: FieldProps) {
  const id = useId()
  const describedBy = [hint && `${id}-hint`, error && `${id}-error`].filter(Boolean).join(' ') || undefined
  return (
    <div className="n-settings-field">
      <label className="n-settings-small-label" htmlFor={id}>
        {label}
      </label>
      <input
        id={id}
        className="n-settings-bare"
        aria-invalid={Boolean(error)}
        aria-describedby={describedBy}
        onChange={(e) => onChange(e.target.value)}
        {...props}
      />
      {hint && (
        <p className="n-settings-help" id={`${id}-hint`}>
          {hint}
        </p>
      )}
      {error && (
        <p className="n-settings-error" id={`${id}-error`} role="alert">
          {error}
        </p>
      )}
    </div>
  )
}

export default Field
