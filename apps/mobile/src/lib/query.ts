import { MutationCache, QueryCache, QueryClient } from '@tanstack/react-query'
import { isOpenChatError } from '@openchat/api-client'

// As the web app's lib/query.ts: every failure passes one listener (set in
// the root layout), so a session ending is handled in one place.
let listener: ((error: unknown) => void) | undefined
export const setErrorListener = (fn: ((error: unknown) => void) | undefined) => {
  listener = fn
}
/** For failures that do not pass through the cache, such as the events stream. */
export const reportError = (error: unknown) => listener?.(error)

export const queryClient = new QueryClient({
  queryCache: new QueryCache({ onError: reportError }),
  mutationCache: new MutationCache({ onError: reportError }),
  defaultOptions: {
    queries: {
      staleTime: 30_000,
      gcTime: 5 * 60_000,
      retry: (failureCount, error) => {
        // Never retry a request the server rejected on its merits.
        if (isOpenChatError(error) && error.status < 500) return false
        return failureCount < 2
      },
    },
    mutations: { retry: false },
  },
})
