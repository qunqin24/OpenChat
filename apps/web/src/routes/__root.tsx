import type { QueryClient } from '@tanstack/react-query'
import { createRootRouteWithContext } from '@tanstack/react-router'
import { lazy, Suspense } from 'react'
import { useTranslation } from 'react-i18next'

import NextShell from '@/components/next/NextShell'

export interface RouterContext {
  queryClient: QueryClient
}

// Devtools are dev-only and code-split so they never reach the production bundle.
const RouterDevtools = import.meta.env.PROD
  ? () => null
  : lazy(() =>
      import('@tanstack/react-router-devtools').then((m) => ({
        default: m.TanStackRouterDevtools,
      })),
    )

const QueryDevtools = import.meta.env.PROD
  ? () => null
  : lazy(() =>
      import('@tanstack/react-query-devtools').then((m) => ({
        default: m.ReactQueryDevtools,
      })),
    )

export const Route = createRootRouteWithContext<RouterContext>()({
  component: RootLayout,
  notFoundComponent: NotFound,
})

function RootLayout() {
  return (
    <>
      <NextShell />
      <Suspense fallback={null}>
        <RouterDevtools position="bottom-right" />
        <QueryDevtools buttonPosition="bottom-left" />
      </Suspense>
    </>
  )
}

function NotFound() {
  const { t } = useTranslation()
  return (
    <main className="n-main">
      <div className="flex h-full flex-col items-center justify-center gap-4">
        <p className="text-sm" style={{ color: 'var(--n-text-2)' }}>
          {t('state.notFound')}
        </p>
        <a className="n-row" style={{ width: 'auto' }} href="/">
          {t('state.backHome')}
        </a>
      </div>
    </main>
  )
}
