import type { UpdateUserRequest, User } from '@openchat/api-client'
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

export function useUpdateUser() {
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: async ({ userId, ...body }: UpdateUserRequest & { userId: string }) =>
      unwrap(
        await api.PATCH('/admin/users/{userId}', { params: { path: { userId } }, body }),
      ),
    onSuccess: (_updated: User) => {
      // Role and status changes move the totals too.
      void queryClient.invalidateQueries({ queryKey: adminKeys.all })
    },
  })
}
