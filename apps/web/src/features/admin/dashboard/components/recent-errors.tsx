import type { AdminDashboard } from '@openchat/api-client'
import { Link } from '@tanstack/react-router'
import { useTranslation } from 'react-i18next'

import { formatLogTime, knownErrorCode } from '@/features/admin/usage-logs/format'
import { formatRelative } from '@/lib/grouping'
import { Empty } from './empty'

/** The page has room for this many; the server sends a few more than fit. */
const MAX = 6

/**
 * The latest failed requests: when, which model through which channel, and
 * what went wrong, in the chat's own word for the code with the provider's
 * (scrubbed) message under it.
 */
export function RecentErrors({ errors }: { errors: AdminDashboard['errors'] }) {
  const { t } = useTranslation()
  if (errors.length === 0) return <Empty message={t('admin.dashboard.errors.empty')} />

  return (
    <ul className="divide-y">
      {errors.slice(0, MAX).map((e) => {
        const known = knownErrorCode(e.errorCode)
        return (
          <li key={e.id} className="space-y-0.5 py-2.5 first:pt-0 last:pb-0">
            <div className="flex items-baseline justify-between gap-3">
              <span className="text-destructive truncate text-sm font-medium">
                {known
                  ? t(`admin.usage.error-code.${known}`)
                  : (e.errorCode ?? t('admin.dashboard.errors.unknown'))}
              </span>
              <span className="text-muted-foreground shrink-0 text-xs" title={formatLogTime(e.createdAt)}>
                {formatRelative(e.createdAt, t)}
              </span>
            </div>
            <p className="text-muted-foreground truncate font-mono text-xs" title={`${e.model} · ${e.channelName}`}>
              {e.model} · {e.channelName}
            </p>
            {e.errorMessage && (
              <p className="text-muted-foreground/80 line-clamp-2 text-xs break-words" title={e.errorMessage}>
                {e.errorMessage}
              </p>
            )}
          </li>
        )
      })}
    </ul>
  )
}

/** The way on to the whole list of failures. */
export function AllErrorsLink() {
  const { t } = useTranslation()
  return (
    <Link
      to="/admin/usage-logs"
      search={{ status: 'error' }}
      className="text-muted-foreground hover:text-foreground text-sm underline-offset-4 hover:underline"
    >
      {t('admin.dashboard.errors.all')} →
    </Link>
  )
}
