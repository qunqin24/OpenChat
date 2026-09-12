import type { QueryClient } from '@tanstack/react-query'
import { createRootRouteWithContext, Outlet } from '@tanstack/react-router'
import { lazy, Suspense } from 'react'
import { useTranslation } from 'react-i18next'

export interface RouterContext {
  queryClient: QueryClient
}

/**
 * Devtools are off by default: their floating buttons sit on top of the UI.
 * Set `VITE_DEVTOOLS=true` in apps/web/.env to get them back.
 *
 * They are lazy, so with the flag off the chunks are never even requested, and
 * a production build drops them regardless of the flag.
 */
const DEVTOOLS = !import.meta.env.PROD && import.meta.env.VITE_DEVTOOLS === 'true'

const RouterDevtools = lazy(() =>
  import('@tanstack/react-router-devtools').then((m) => ({
    default: m.TanStackRouterDevtools,
  })),
)

const QueryDevtools = lazy(() =>
  import('@tanstack/react-query-devtools').then((m) => ({
    default: m.ReactQueryDevtools,
  })),
)

export const Route = createRootRouteWithContext<RouterContext>()({
  component: RootLayout,
  notFoundComponent: NotFound,
})

/**
 * Deliberately bare: the app shell lives in the `_authed` layout, so the
 * sign-in and sign-up screens render without a sidebar.
 */
function RootLayout() {
  return (
    <>
      <Outlet />
      {DEVTOOLS && (
        <Suspense fallback={null}>
          {/* Both live on the right: the sidebar owns the bottom-left corner,
              where a floating button would sit on top of the appearance and
              language rows. */}
          <RouterDevtools position="top-right" />
          <QueryDevtools buttonPosition="bottom-right" />
        </Suspense>
      )}
    </>
  )
}

function NotFound() {
  const { t } = useTranslation()
  return (
    <div className="n-auth">
      <div className="flex flex-col items-center gap-4">
        <p className="text-sm" style={{ color: 'var(--n-text-2)' }}>
          {t('state.notFound')}
        </p>
        <a className="n-auth-link" href="/">
          {t('state.backHome')}
        </a>
      </div>
    </div>
  )
}
