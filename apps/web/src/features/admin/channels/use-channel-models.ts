import { OpenChatError, type components } from '@openchat/api-client'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'

import { api, unwrap } from '@/lib/api'
import { catalogKeys } from '@/features/admin/models/use-catalog'
import { adminKeys } from '@/features/admin/use-admin'

type Schemas = components['schemas']

export type ChannelModel = Schemas['ChannelModel']
export type ChannelModelRefresh = Schemas['ChannelModelRefresh']
export type ChannelConnection = Schemas['ChannelConnection']
export type ChannelModelInput = Schemas['ChannelModelInput']

// Previews are ephemeral and may contain an unsaved credential. Keep the
// request outside Query's mutation cache; closing the panel aborts it.
export async function previewChannelModels(connection: ChannelConnection, signal: AbortSignal) {
  return unwrap(await api.POST('/admin/channels/models/preview', { body: connection, signal }))
}

export function channelModelErrorKey(error: unknown) {
  if (error instanceof OpenChatError) {
    switch (error.code) {
      case 'channel_key_missing': return 'admin.channels.models-enter-key' as const
      case 'channel_key_rejected': return 'admin.channels.models-key-rejected' as const
      case 'channel_base_url_invalid': return 'admin.channels.models-invalid-url' as const
      case 'channel_listed_nothing': return 'admin.channels.models-list-empty' as const
      case 'channel_unreachable': return 'admin.channels.models-unreachable' as const
    }
  }
  return 'admin.channels.models-fetch-failed' as const
}

/**
 * A channel's models live under the channel's own key, because that is what
 * they belong to: invalidating the channel list sweeps the model lists hanging
 * below it, which is the right thing to happen when a channel's protocol or
 * credential changes under them.
 *
 * The flip side is that every mutation here has to invalidate the channel list
 * `exact`ly — a prefix invalidation would also throw away the model list the
 * mutation has just finished updating.
 */
export const channelModelKeys = {
  all: (channelId: string) => [...adminKeys.channels(), channelId, 'models'] as const,
}

/**
 * Everything the channel offers, in one response.
 *
 * No `placeholderData`, and deliberately so: the key carries no filter. The
 * search box filters the array client-side, and `channelId` is an identity
 * rather than a filter — holding the previous value on screen would mean
 * showing one channel's models inside another channel's panel.
 */
export function useChannelModels(channelId: string) {
  return useQuery({
    queryKey: channelModelKeys.all(channelId),
    queryFn: async ({ signal }) =>
      unwrap(
        await api.GET('/admin/channels/{channelId}/models', {
          params: { path: { channelId } },
          signal,
        }),
      ),
  })
}

/**
 * What every mutation below has to tell the cache.
 *
 * A hook rather than three copies of the same three lines — the comments are
 * the point, and three drifting copies of them is how a cache starts lying.
 */
function useInvalidateChannelModels(channelId: string) {
  const queryClient = useQueryClient()

  return () => {
    void queryClient.invalidateQueries({ queryKey: channelModelKeys.all(channelId) })
    // The channel row shows `modelCount`, which is a count of its enabled
    // models — so the row in the table behind this panel is now out of date.
    // `exact` stops this from also invalidating the list above, which the line
    // before it has already taken care of.
    void queryClient.invalidateQueries({ queryKey: adminKeys.channels(), exact: true })
    // The catalogue answers "what can this installation actually reach" out of
    // the channels' model lists, so it moved too. It lives under its own key
    // and would otherwise go on reporting the reach this change just altered.
    void queryClient.invalidateQueries({ queryKey: catalogKeys.models() })
  }
}

/**
 * Ask the endpoint what it serves. The first outbound call this server makes on
 * the operator's behalf, so it needs the stored credential — without one the
 * button that triggers it is disabled rather than allowed to fail.
 *
 * The response is a tally, not a list, so the list has to be re-read.
 */
export function useRefreshChannelModels(channelId: string) {
  const invalidate = useInvalidateChannelModels(channelId)

  return useMutation({
    mutationFn: async () =>
      unwrap(
        await api.POST('/admin/channels/{channelId}/models/refresh', {
          params: { path: { channelId } },
        }),
      ),
    onSuccess: () => invalidate(),
  })
}

/** For an endpoint that publishes no list, or omits a model it actually serves. */
export function useAddChannelModel(channelId: string) {
  const invalidate = useInvalidateChannelModels(channelId)

  return useMutation({
    mutationFn: async (modelId: string) =>
      unwrap(
        await api.POST('/admin/channels/{channelId}/models', {
          params: { path: { channelId } },
          body: { modelId },
        }),
      ),
    onSuccess: () => invalidate(),
  })
}

/**
 * Only `manual` rows can be removed. A fetched row is the endpoint's statement
 * about itself — the way to be rid of one is to switch it off, or to refresh
 * after the endpoint stops listing it.
 */
export function useDeleteChannelModel(channelId: string) {
  const invalidate = useInvalidateChannelModels(channelId)

  return useMutation({
    mutationFn: async (modelId: string) =>
      unwrap(
        await api.DELETE('/admin/channels/{channelId}/models/{modelId}', {
          params: { path: { channelId, modelId } },
        }),
      ),
    onSuccess: () => invalidate(),
  })
}

/** Offering one model, or ceasing to. */
export function useUpdateChannelModel(channelId: string) {
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: async ({ modelId, enabled }: { modelId: string; enabled: boolean }) =>
      unwrap(
        await api.PATCH('/admin/channels/{channelId}/models/{modelId}', {
          params: { path: { channelId, modelId } },
          body: { enabled },
        }),
      ),
    onSuccess: (model) => {
      // The response is the row as it now stands, so the list is patched in
      // place instead of refetched. A relay's several hundred rows should not
      // drop into a loading state because one switch moved.
      queryClient.setQueryData<ChannelModel[]>(channelModelKeys.all(channelId), (current) =>
        current?.map((row) => (row.modelId === model.modelId ? model : row)),
      )
      void queryClient.invalidateQueries({ queryKey: adminKeys.channels(), exact: true })
      void queryClient.invalidateQueries({ queryKey: catalogKeys.models() })
    },
  })
}

/**
 * How many PATCHes are in flight at once during an enable-all.
 *
 * There is no bulk endpoint, and "all" can mean several hundred rows, so this
 * is a lot of small requests either way. Sending them in waves keeps the
 * browser's connection pool — and the endpoint — from being handed the whole
 * pile at once, at the cost of a second or so on the largest lists.
 */
const PATCH_WAVE = 6

/**
 * Switching a whole run of models on or off.
 *
 * The caller passes the ids it means, which is what lets the buttons act on
 * what the search box is currently showing rather than on rows nobody can see.
 */
export function useSetChannelModelsEnabled(channelId: string) {
  const invalidate = useInvalidateChannelModels(channelId)

  return useMutation({
    mutationFn: async ({ modelIds, enabled }: { modelIds: string[]; enabled: boolean }) => {
      for (let index = 0; index < modelIds.length; index += PATCH_WAVE) {
        await Promise.all(
          modelIds.slice(index, index + PATCH_WAVE).map(async (modelId) =>
            unwrap(
              await api.PATCH('/admin/channels/{channelId}/models/{modelId}', {
                params: { path: { channelId, modelId } },
                body: { enabled },
              }),
            ),
          ),
        )
      }
    },
    // `onSettled`, not `onSuccess`: a run that fails halfway has still changed
    // every row before the one that failed, and leaving the list showing a
    // state that is only half true is worse than one extra request.
    onSettled: () => invalidate(),
  })
}
