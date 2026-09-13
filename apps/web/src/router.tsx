import { createRouter } from '@tanstack/react-router'

import { queryClient } from '@/lib/query'
import { routeTree } from '@/routeTree.gen'

export const router = createRouter({
  routeTree,
  context: { queryClient },
  defaultPreload: 'intent',
  defaultPreloadStaleTime: 0, // let TanStack Query own caching
  // Chat parks its transcript at the tail before paint. Router restoration
  // would overwrite that position with the previous conversation's offset.
  scrollRestoration: ({ location }) =>
    location.pathname !== '/' && !location.pathname.startsWith('/chat/'),
})

declare module '@tanstack/react-router' {
  interface Register {
    router: typeof router
  }
}
