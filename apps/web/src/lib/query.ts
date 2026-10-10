import { MutationCache, QueryCache, QueryClient } from '@tanstack/react-query'
import { isOpenChatError } from '@openchat/api-client'

/**
 * Every failed query and mutation is shown to one listener (set once, in
 * main.tsx), so something that applies to all of them - the session having
 * ended - is handled in one place instead of in each call site. It does not
 * replace a call's own error handling.
 */
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
      refetchOnWindowFocus: false,
      retry: (failureCount, error) => {
        // Never retry a request the server rejected on its merits.
        if (isOpenChatError(error) && error.status < 500) return false
        return failureCount < 2
      },
    },
    mutations: { retry: false },
  },
})
