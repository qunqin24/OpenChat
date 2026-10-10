import type {
  BulkUpdateUsersRequest,
  CreateChannelRequest,
  CreateUserRequest,
  UpdateChannelRequest,
  UpdateUserRequest,
} from '@openchat/api-client'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'

import { api, unwrap } from '@/lib/api'

export const adminKeys = {
  all: ['admin'] as const,
  stats: (days: number) => [...adminKeys.all, 'stats', days] as const,
  users: (page: number, search: string) => [...adminKeys.all, 'users', page, search] as const,
  system: () => [...adminKeys.all, 'system'] as const,
  /** Shared with features/admin/models, which owns the rest of this subtree. */
  catalog: () => [...adminKeys.all, 'catalog'] as const,
  channels: () => [...adminKeys.all, 'channels'] as const,
  webSearch: () => [...adminKeys.all, 'web-search'] as const,
  storage: () => [...adminKeys.all, 'storage'] as const,
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

/**
 * The whole list in one response — there are a handful of channels at most, so
 * there is nothing to page through and no filter in the key. That is also why
 * none of the channel hooks need `placeholderData`: the key never changes.
 */
export function useAdminChannels() {
  return useQuery({
    queryKey: adminKeys.channels(),
    queryFn: async ({ signal }) => unwrap(await api.GET('/admin/channels', { signal })),
  })
}

export function useCreateChannel() {
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: async (body: CreateChannelRequest) =>
      unwrap(await api.POST('/admin/channels', { body })),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: adminKeys.channels() })
      // A channel decides which models are reachable, so the catalogue's
      // answer changed as well — it lives under a different key and would
      // otherwise go on reporting the reach this channel just altered.
      void queryClient.invalidateQueries({ queryKey: adminKeys.catalog() })
    },
  })
}

/**
 * A partial update: only the fields present in `body` change.
 *
 * `apiKey` in particular is write-only — the server never hands a key back, so
 * an absent `apiKey` means "keep the stored one" and an empty string means
 * "clear it". Callers that are not deliberately changing the credential must
 * leave the property off the object entirely rather than passing `''`.
 */
export function useUpdateChannel() {
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: async ({ channelId, ...body }: UpdateChannelRequest & { channelId: string }) =>
      unwrap(
        await api.PATCH('/admin/channels/{channelId}', {
          params: { path: { channelId } },
          body,
        }),
      ),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: adminKeys.channels() })
      // A channel decides which models are reachable, so the catalogue's
      // answer changed as well — it lives under a different key and would
      // otherwise go on reporting the reach this channel just altered.
      void queryClient.invalidateQueries({ queryKey: adminKeys.catalog() })
    },
  })
}

/**
 * Unlike an account, a channel owns no user data — deleting one drops the
 * connection and its key, and leaves every conversation untouched.
 */
export function useDeleteChannel() {
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: async (channelId: string) =>
      unwrap(await api.DELETE('/admin/channels/{channelId}', { params: { path: { channelId } } })),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: adminKeys.channels() })
      // A channel decides which models are reachable, so the catalogue's
      // answer changed as well — it lives under a different key and would
      // otherwise go on reporting the reach this channel just altered.
      void queryClient.invalidateQueries({ queryKey: adminKeys.catalog() })
    },
  })
}
