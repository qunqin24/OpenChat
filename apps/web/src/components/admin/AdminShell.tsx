import { Link, Outlet, useNavigate } from '@tanstack/react-router'
import { ArrowLeft, LayoutDashboard, Users } from 'lucide-react'
import { useTranslation } from 'react-i18next'

/**
 * The admin panel's own frame. It reuses `.prism-next` — the same fixed,
 * two-column shell as the chat UI — so every `.n-*` rule in the theme applies
 * here without being redefined for a second layout.
 */
export function AdminShell() {
  const { t } = useTranslation()
  const navigate = useNavigate()

  return (
    <div className="prism-next">
      <aside className="n-sidebar">
        <div className="px-2 pt-3">
          <div className="n-brand">
            <span className="n-brand-name">{t('admin.title')}</span>
          </div>

          <Link
            to="/admin"
            activeOptions={{ exact: true }}
            className="n-row"
            activeProps={{ className: 'n-row active' }}
          >
            <LayoutDashboard className="h-4 w-4 shrink-0" />
            <span className="n-row-label">{t('admin.nav.dashboard')}</span>
          </Link>

          <Link to="/admin/users" className="n-row" activeProps={{ className: 'n-row active' }}>
            <Users className="h-4 w-4 shrink-0" />
            <span className="n-row-label">{t('admin.nav.users')}</span>
          </Link>
        </div>

        <div className="n-sidebar-scroll" />

        <div className="p-2">
          <button type="button" className="n-row" onClick={() => void navigate({ to: '/' })}>
            <ArrowLeft className="h-4 w-4 shrink-0" />
            <span className="n-row-label">{t('admin.back-to-chat')}</span>
          </button>
        </div>
      </aside>

      <div className="n-route-motion">
        <Outlet />
      </div>
    </div>
  )
}

export default AdminShell
