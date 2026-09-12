import type { ReactNode } from 'react'
import { useTranslation } from 'react-i18next'

interface AuthShellProps {
  title: string
  subtitle: string
  children: ReactNode
  /** Sign-in ↔ sign-up switch, shown under the form. */
  footer: ReactNode
}

/** Shared frame for the sign-in and sign-up screens. */
export function AuthShell({ title, subtitle, children, footer }: AuthShellProps) {
  const { t } = useTranslation()

  return (
    <div className="n-auth">
      <div className="n-auth-card">
        <div className="n-auth-brand">{t('app.name')}</div>

        <h1 className="n-auth-title">{title}</h1>
        <p className="n-auth-sub">{subtitle}</p>

        {children}

        <div className="n-auth-foot">{footer}</div>
      </div>
    </div>
  )
}

export default AuthShell
