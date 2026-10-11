import type { components, UpdateModerationSettingsRequest } from '@openchat/api-client'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'

import { api, unwrap } from '@/lib/api'
import { adminKeys } from '@/features/admin/use-admin'
import type { ModerationCategory } from './moderation-schema'

type Schemas = components['schemas']

export type ModerationSettings = Schemas['ModerationSettings']
export type ModerationEvent = Schemas['ModerationEvent']
export type GuestBan = Schemas['GuestBan']

/** What the log page narrows the list by. An absent field is no constraint. */
export type ModerationFilters = {
  userId?: string
  guestIp?: string
  category?: ModerationCategory
}

/** Under `admin`, so the broad invalidation the admin mutations do reaches it. */
export const moderationKeys = {
  all: [...adminKeys.all, 'moderation'] as const,
  settings: () => [...moderationKeys.all, 'settings'] as const,
  bans: () => [...moderationKeys.all, 'bans'] as const,
  events: (cursor: string | undefined, limit: number, filters: ModerationFilters) =>
    [...moderationKeys.all, 'events', cursor ?? null, limit, filters] as const,
}

export function useModerationSettings() {
  return useQuery({
    queryKey: moderationKeys.settings(),
    queryFn: async ({ signal }) => unwrap(await api.GET('/admin/moderation', { signal })),
  })
}

/** Saves a change; the server answers with the whole settings, which replace the cached ones. */
export function useUpdateModeration() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async (body: UpdateModerationSettingsRequest) => unwrap(await api.PATCH('/admin/moderation', { body })),
    onSuccess: (next) => queryClient.setQueryData(moderationKeys.settings(), next),
  })
}

export function useGuestBans() {
  return useQuery({
    queryKey: moderationKeys.bans(),
    queryFn: async ({ signal }) => unwrap(await api.GET('/admin/moderation/bans', { signal })),
  })
}

/** Bans the address of a logged guest message, for the length the settings give. */
export function useBanGuest() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async (eventId: string) => unwrap(await api.POST('/admin/moderation/bans', { body: { eventId } })),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: moderationKeys.all })
    },
  })
}

export function useLiftBan() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async (ip: string) =>
      unwrap(await api.DELETE('/admin/moderation/bans', { params: { query: { ip } } })),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: moderationKeys.all })
    },
  })
}

/**
 * One page of the log, newest first. Reached by cursor, which only means
 * something with the filters it was issued under, so they are in the key.
 */
export function useModerationEvents({
  cursor,
  limit,
  filters,
}: {
  cursor: string | undefined
  limit: number
  filters: ModerationFilters
}) {
  return useQuery({
    queryKey: moderationKeys.events(cursor, limit, filters),
    queryFn: async ({ signal }) =>
      unwrap(
        await api.GET('/admin/moderation/events', {
          params: {
            query: {
              limit,
              ...(cursor ? { cursor } : {}),
              ...(filters.userId ? { userId: filters.userId } : {}),
              ...(filters.guestIp ? { guestIp: filters.guestIp } : {}),
              ...(filters.category ? { category: filters.category } : {}),
            },
          },
          signal,
        }),
      ),
    placeholderData: (previous) => previous,
  })
}
