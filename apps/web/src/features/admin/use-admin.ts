import type {
  BulkUpdateUsersRequest,
  CreateUserRequest,
  UpdateUserRequest,
} from '@openchat/api-client'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'

import { api, unwrap } from '@/lib/api'

export const adminKeys = {
  all: ['admin'] as const,
  stats: (days: number) => [...adminKeys.all, 'stats', days] as const,
  users: (page: number, search: string) => [...adminKeys.all, 'users', page, search] as const,
}

export function useAdminStats(days = 30) {
  return useQuery({
    queryKey: adminKeys.stats(days),
    queryFn: async ({ signal }) =>
      unwrap(await api.GET('/admin/stats', { params: { query: { days } }, signal })),
    // Changing the window changes the key, which would otherwise drop `data` to
    // undefined until the new request lands — the charts would empty out and
    // redraw on every switch. Keeping the previous response on screen also
    // leaves the totals correct throughout, since they do not depend on `days`
    // at all; only the trend does.
    placeholderData: (previous) => previous,
  })
}

export function useAdminUsers({
  page,
  search,
  limit = 20,
}: {
  page: number
  search: string
  limit?: number
}) {
  return useQuery({
    queryKey: adminKeys.users(page, search),
    queryFn: async ({ signal }) =>
      unwrap(
        await api.GET('/admin/users', {
          params: { query: { page, limit, ...(search ? { search } : {}) } },
          signal,
        }),
      ),
    // Keeps the previous page on screen while the next one loads, so the table
    // does not collapse to a spinner on every keystroke or page change.
    placeholderData: (previous) => previous,
  })
}

export function useBulkUpdateUsers() {
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: async (body: BulkUpdateUsersRequest) =>
      unwrap(await api.POST('/admin/users/bulk', { body })),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: adminKeys.all })
    },
  })
}

/**
 * The single-row counterpart to the bulk endpoint. Refuses the same two
 * changes — removing your own admin access, or the last enabled admin — with a
 * 409, which the caller turns into a toast.
 */
export function useUpdateUser() {
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: async ({ userId, ...body }: UpdateUserRequest & { userId: string }) =>
      unwrap(await api.PATCH('/admin/users/{userId}', { params: { path: { userId } }, body })),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: adminKeys.all })
    },
  })
}

/**
 * Creating an account here does not sign anyone in — unlike registration, the
 * caller is an admin acting on somebody else's behalf, so no cookie changes
 * hands.
 */
export function useCreateUser() {
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: async (body: CreateUserRequest) =>
      unwrap(await api.POST('/admin/users', { body })),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: adminKeys.all })
    },
  })
}
