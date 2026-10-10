import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'

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
    onSuccess: (user: SessionUser) => {
      queryClient.setQueryData(sessionQueryOptions.queryKey, user)
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

export function useChangePassword() {
  return useMutation({
    mutationFn: (input: ChangePasswordInput) => authApi.changePassword(input),
  })
}
