import type { User } from '@openchat/api-client'
import { ChevronLeft, ChevronRight, Search, ShieldCheck, ShieldOff, UserCheck, UserX } from 'lucide-react'
import { useEffect, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { useSession } from '@/features/auth/use-auth'
import { useAdminUsers, useUpdateUser } from '@/features/admin/use-admin'
import { formatRelative } from '@/lib/grouping'
import Tooltip, { TooltipGroup } from '@/components/next/Tooltip'

const PAGE_SIZE = 20

export function UsersPage() {
  const { t } = useTranslation()
  const { data: session } = useSession()
  const updateUser = useUpdateUser()

  const [input, setInput] = useState('')
  const [search, setSearch] = useState('')
  const [page, setPage] = useState(1)

  // Debounced so typing does not fire a request per keystroke.
  useEffect(() => {
    const timer = window.setTimeout(() => {
      setSearch(input.trim())
      setPage(1)
    }, 300)
    return () => window.clearTimeout(timer)
  }, [input])

  const { data, isPending, isError } = useAdminUsers({ page, search, limit: PAGE_SIZE })

  const pages = useMemo(
    () => Math.max(1, Math.ceil((data?.total ?? 0) / PAGE_SIZE)),
    [data?.total],
  )

  const change = (user: User, body: { role?: User['role']; disabled?: boolean }) =>
    updateUser.mutate({ userId: user.id, ...body })

  return (
    <main className="n-main">
      <div className="n-topbar">
        <div className="truncate font-medium">{t('admin.nav.users')}</div>
      </div>

      <div className="n-scroll">
        <div className="n-admin-page">
          <div className="n-log-panel">
            <div className="n-admin-toolbar">
              <div className="n-admin-search">
                <Search className="h-4 w-4 shrink-0" style={{ color: 'var(--n-text-3)' }} />
                <input
                  className="n-admin-search-input"
                  value={input}
                  placeholder={t('admin.users.search')}
                  aria-label={t('admin.users.search')}
                  onChange={(event) => setInput(event.target.value)}
                />
              </div>
              <span className="n-admin-count">
                {t('admin.users.total', { n: data?.total ?? 0 })}
              </span>
            </div>

            <div className="n-admin-table-wrap">
              <table className="n-admin-table">
                <thead>
                  <tr>
                    <th>{t('admin.users.account')}</th>
                    <th>{t('admin.users.role')}</th>
                    <th className="num">{t('admin.users.conversations')}</th>
                    <th>{t('admin.users.last-seen')}</th>
                    <th>{t('admin.users.joined')}</th>
                    <th className="actions">{t('admin.users.actions')}</th>
                  </tr>
                </thead>
                <tbody>
                  {isError && (
                    <tr>
                      <td colSpan={6} className="n-admin-empty">
                        {t('state.error')}
                      </td>
                    </tr>
                  )}

                  {!isError && isPending && (
                    <tr>
                      <td colSpan={6} className="n-admin-empty">
                        {t('state.loading')}
                      </td>
                    </tr>
                  )}

                  {!isError && !isPending && data?.items.length === 0 && (
                    <tr>
                      <td colSpan={6} className="n-admin-empty">
                        {t('admin.users.none')}
                      </td>
                    </tr>
                  )}

                  <TooltipGroup>
                    {data?.items.map((user) => {
                      const isSelf = user.id === session?.id
                      const isAdmin = user.role === 'admin'

                      return (
                        <tr key={user.id} className={user.disabled ? 'disabled' : undefined}>
                          <td>
                            <div className="n-admin-account">
                              <span className="n-admin-name">{user.name}</span>
                              <span className="n-admin-email">{user.email}</span>
                            </div>
                          </td>
                          <td>
                            <span className={`n-admin-tag${isAdmin ? ' admin' : ''}`}>
                              {t(`admin.users.role-${user.role}`)}
                            </span>
                            {user.disabled && (
                              <span className="n-admin-tag off">{t('admin.users.disabled')}</span>
                            )}
                          </td>
                          <td className="num">{user.conversationCount ?? 0}</td>
                          <td>{formatRelative(user.lastSeenAt ?? undefined, t) || '—'}</td>
                          <td>{formatRelative(user.createdAt, t)}</td>
                          <td className="actions">
                            {/* Self-changes are refused by the server; disabling
                                them here explains why instead of failing later. */}
                            <div className="n-admin-actions">
                              <Tooltip
                                label={
                                  isSelf
                                    ? t('admin.users.self-locked')
                                    : isAdmin
                                      ? t('admin.users.demote')
                                      : t('admin.users.promote')
                                }
                              >
                                <span className="n-tip-wrap">
                                  <button
                                    type="button"
                                    className="n-icon-btn sm"
                                    disabled={isSelf || updateUser.isPending}
                                    onClick={() =>
                                      change(user, { role: isAdmin ? 'user' : 'admin' })
                                    }
                                  >
                                    {isAdmin ? (
                                      <ShieldOff className="h-3.5 w-3.5" />
                                    ) : (
                                      <ShieldCheck className="h-3.5 w-3.5" />
                                    )}
                                  </button>
                                </span>
                              </Tooltip>

                              <Tooltip
                                label={
                                  isSelf
                                    ? t('admin.users.self-locked')
                                    : user.disabled
                                      ? t('admin.users.enable')
                                      : t('admin.users.disable')
                                }
                              >
                                <span className="n-tip-wrap">
                                  <button
                                    type="button"
                                    className="n-icon-btn sm"
                                    disabled={isSelf || updateUser.isPending}
                                    onClick={() => change(user, { disabled: !user.disabled })}
                                  >
                                    {user.disabled ? (
                                      <UserCheck className="h-3.5 w-3.5" />
                                    ) : (
                                      <UserX className="h-3.5 w-3.5" />
                                    )}
                                  </button>
                                </span>
                              </Tooltip>
                            </div>
                          </td>
                        </tr>
                      )
                    })}
                  </TooltipGroup>
                </tbody>
              </table>
            </div>

            {pages > 1 && (
              <div className="n-log-pager">
                <button
                  type="button"
                  className="n-pager-btn"
                  disabled={page <= 1}
                  aria-label={t('admin.users.previous')}
                  onClick={() => setPage((value) => Math.max(1, value - 1))}
                >
                  <ChevronLeft className="h-3.5 w-3.5" />
                </button>
                <span className="n-log-pager-label">
                  {page} / {pages}
                </span>
                <button
                  type="button"
                  className="n-pager-btn"
                  disabled={page >= pages}
                  aria-label={t('admin.users.next')}
                  onClick={() => setPage((value) => Math.min(pages, value + 1))}
                >
                  <ChevronRight className="h-3.5 w-3.5" />
                </button>
              </div>
            )}
          </div>

          {updateUser.isError && (
            <p className="n-admin-alert" role="alert">
              {t('admin.users.update-failed')}
            </p>
          )}
        </div>
      </div>
    </main>
  )
}

export default UsersPage
