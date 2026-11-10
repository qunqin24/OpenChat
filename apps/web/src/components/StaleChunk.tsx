import { ErrorComponent, type ErrorComponentProps } from '@tanstack/react-router'
import { Component, useEffect, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'

import { isChunkLoadError, noteChunkError } from '@/lib/build-check'

/**
 * What the page does when a piece of the app cannot be fetched - above all a
 * page loaded before an upgrade, asking for pieces the server no longer has
 * (lib/build-check.ts, isChunkLoadError): it says so and offers to reload,
 * where there would otherwise be the router's "Something went wrong". Never a
 * reload by itself, which would throw away a half-written message.
 */

/**
 * Around a part that is fetched when first shown (React.lazy): if it cannot be
 * fetched, `fallback` stands in its place and the rest of the page - the
 * composer and what is typed in it - stays. Any other error is not this
 * boundary's and goes on to the next one.
 */
export class ChunkBoundary extends Component<{ fallback: ReactNode; children: ReactNode }, { error: unknown }> {
  state: { error: unknown } = { error: null }

  static getDerivedStateFromError(error: unknown) {
    return { error }
  }

  componentDidCatch(error: unknown) {
    noteChunkError(error)
  }

  render() {
    const { error } = this.state
    if (error === null) return this.props.children
    if (!isChunkLoadError(error)) throw error
    return this.props.fallback
  }
}

/** In place of a part that could not be fetched. */
export function StaleNote({ className }: { className: string }) {
  const { t } = useTranslation()
  return <p className={className}>{t('app.stale.message')}</p>
}

/**
 * The router's error screen. A route whose code could not be fetched says so,
 * with the way to reload; everything else is the router's own screen, as
 * before.
 */
export function RouteError(props: ErrorComponentProps) {
  const { t } = useTranslation()
  const stale = isChunkLoadError(props.error)
  useEffect(() => {
    if (stale) noteChunkError(props.error)
  }, [stale, props.error])

  if (!stale) return <ErrorComponent {...props} />
  return (
    <div className="n-auth">
      <div className="m-auto flex flex-col items-center gap-4">
        <p className="text-sm" style={{ color: 'var(--n-text-2)' }}>
          {t('app.stale.message')}
        </p>
        <a
          className="n-auth-link"
          href={window.location.href}
          onClick={(event) => {
            event.preventDefault()
            window.location.reload()
          }}
        >
          {t('app.updated.reload')}
        </a>
      </div>
    </div>
  )
}
