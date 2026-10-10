import type { ReactNode } from 'react'

interface AuthShellProps {
  /** Includes the app name, as OpenWebUI's "Sign in to {{WEBUI_NAME}}" does. */
  title: string
  children: ReactNode
}

/**
 * Shared frame for the sign-in and sign-up screens, after OpenWebUI's
 * src/routes/auth/+page.svelte: a plain white/black page and one centred
 * column. There is no card, logo, or subtitle — the title carries the app
 * name.
 */
export function AuthShell({ title, children }: AuthShellProps) {
  return (
    <div className="n-auth">
      <div className="n-auth-column">
        <div className="n-auth-head">
          <h1 className="n-auth-title">{title}</h1>
        </div>
        {children}
      </div>
    </div>
  )
}

export default AuthShell
