import { useMutation, useQuery, useQueryClient, type QueryClient } from '@tanstack/react-query'
import { useNavigate } from '@tanstack/react-router'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { authOptionsQueryOptions } from '@/hooks/use-auth-options'
import { api, unwrap } from '@/lib/api'
import { putAvatar, squareAvatar } from '@/lib/avatar'
import { queryKeys } from '@/lib/query-keys'
import { forgetQueryCache } from '@/lib/query-persist'
import { resetChatStore } from '@/stores/chat-store'
import {
  authApi,
  type ChangePasswordInput,
  type LoginInput,
  type RegisterInput,
  type SessionUser,
  type UpdateProfileInput,
} from './index'

/**
 * The session is server state, not client state: with an HttpOnly cookie the
 * only way to know whether you are signed in is to ask. So it lives in
 * TanStack Query like any other resource, not in Zustand.
 */
export const sessionQueryOptions = {
  queryKey: queryKeys.auth.session,
  queryFn: () => authApi.session(),
  staleTime: 5 * 60_000,
  retry: false,
}

/**
 * Who is signed in, for the route guards. A session held for less than its
 * stale time costs no request; an older one is asked of the server again,
 * because an account can be disabled or its password changed while a tab sits
 * open. If that check cannot be made (offline), the last answer stands rather
 * than the page failing; with no earlier answer there is nothing to fall back to.
 * (`ensureQueryData` would hand back the old answer however old it was.)
 */
export async function currentSession(queryClient: QueryClient): Promise<SessionUser | null> {
  try {
    return await queryClient.fetchQuery(sessionQueryOptions)
  } catch (error) {
    const held = queryClient.getQueryData<SessionUser | null>(sessionQueryOptions.queryKey)
    if (held !== undefined) return held
    throw error
  }
}

export function useSession() {
  return useQuery(sessionQueryOptions)
}

/**
 * Nothing of whoever was here before - their queries, their chat state, the
 * copy kept in the browser's storage - may be seen by the next. Used on both
 * sides of a session: signing out, and signing in (the tab may have held
 * another account's, or a session that ended without the page being told).
 */
function dropEverything(queryClient: QueryClient) {
  queryClient.clear()
  resetChatStore()
  forgetQueryCache()
}

export function useLogin() {
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: (input: LoginInput) => authApi.login(input),
    onSuccess: (user: SessionUser) => {
      dropEverything(queryClient)
      queryClient.setQueryData(sessionQueryOptions.queryKey, user)
    },
  })
}

/** A passkey sign-in; resolves to null when the person closed the browser's prompt. */
export function usePasskeyLogin() {
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: () => authApi.passkeyLogin(),
    onSuccess: (user) => {
      if (!user) return
      dropEverything(queryClient)
      queryClient.setQueryData(sessionQueryOptions.queryKey, user)
    },
  })
}

export function useRegister() {
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: (input: RegisterInput) => authApi.register(input),
    onSuccess: (outcome) => {
      // A pending account has no session to remember.
      if (outcome.status === 'signed_in') {
        dropEverything(queryClient)
        queryClient.setQueryData(sessionQueryOptions.queryKey, outcome.user)
      }
    },
  })
}

/**
 * Signing out, at once. The request goes first, with the cookie it ends; the
 * page does not wait for it. The session is known to be gone, so the sign-in
 * form does not ask the server again, and everything of the account is
 * dropped only once the page has left - dropped earlier, the open chat would
 * fetch it all again on the way out. Waiting on each of those in turn is what
 * made a sign-out take a while on a server far away. Should the request fail,
 * the session may still be good: the person is told, and can sign out again.
 */
export function useSignOut() {
  const queryClient = useQueryClient()
  const navigate = useNavigate()
  const { t } = useTranslation()

  return async () => {
    const request = authApi.logout()
    queryClient.setQueryData(sessionQueryOptions.queryKey, null)
    void queryClient.prefetchQuery(authOptionsQueryOptions)
    await navigate({ to: '/login', replace: true })
    const options = queryClient.getQueryData(authOptionsQueryOptions.queryKey)
    dropEverything(queryClient)
    queryClient.setQueryData(sessionQueryOptions.queryKey, null)
    if (options) queryClient.setQueryData(authOptionsQueryOptions.queryKey, options)
    try {
      await request
    } catch {
      toast.error(t('auth.logout-failed'))
    }
  }
}

export function useUpdateProfile() {
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: (input: UpdateProfileInput) => authApi.updateProfile(input),
    onSuccess: (user: SessionUser) => {
      queryClient.setQueryData(sessionQueryOptions.queryKey, user)
      // The admin user list shows the same row under a different key.
      void queryClient.invalidateQueries({ queryKey: ['admin'] })
    },
  })
}

/** Sets the account's picture from an image file (cut to a square first), or removes it with `null`. */
export function useSetAvatar() {
  const queryClient = useQueryClient()

  return useMutation({
    // The picture shows its own progress where it is.
    meta: { silent: true },
    mutationFn: async (file: File | null): Promise<SessionUser> => {
      if (file === null) return unwrap(await api.DELETE('/me/avatar')) as SessionUser
      return (await putAvatar(await squareAvatar(file))) as SessionUser
    },
    onSuccess: (user: SessionUser) => {
      queryClient.setQueryData(sessionQueryOptions.queryKey, user)
      // The admin lists show the same account's picture under their own keys.
      void queryClient.invalidateQueries({ queryKey: ['admin'] })
    },
  })
}

export function useChangePassword() {
  return useMutation({
    mutationFn: (input: ChangePasswordInput) => authApi.changePassword(input),
  })
}
