import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'

import { api, unwrap } from '@/lib/api'
import { putAvatar, squareAvatar } from '@/lib/avatar'
import { forgetQueryCache } from '@/lib/query-persist'
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
  queryKey: ['auth', 'session'] as const,
  queryFn: () => authApi.session(),
  staleTime: 5 * 60_000,
  retry: false,
}

export function useSession() {
  return useQuery(sessionQueryOptions)
}

export function useLogin() {
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: (input: LoginInput) => authApi.login(input),
    onSuccess: (user: SessionUser) => {
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
        queryClient.setQueryData(sessionQueryOptions.queryKey, outcome.user)
      }
    },
  })
}

export function useLogout() {
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: () => authApi.logout(),
    onSuccess: () => {
      // Everything cached was scoped to the person signing out, in memory and
      // in the browser's storage.
      queryClient.clear()
      forgetQueryCache()
    },
  })
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
