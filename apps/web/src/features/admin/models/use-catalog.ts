import type { components } from '@openchat/api-client'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'

import { api, unwrap } from '@/lib/api'
import { queryKeys } from '@openchat/chat-core/query-keys'
import { useAdminChannels } from '@/features/admin/use-admin'

type Schemas = components['schemas']

export type CatalogStatus = Schemas['CatalogStatus']
export type CatalogModel = Schemas['CatalogModel']
export type ModelChannel = Schemas['ModelChannel']
export type ModelSettings = Schemas['ModelSettings']
export type ModelOverride = Schemas['ModelOverride']
export type ModelKind = Schemas['ModelKind']
export type InputModality = Schemas['InputModality']
export type CapacityPeriod = Schemas['CapacityPeriod']
export type ModelCapacity = Schemas['ModelCapacity']

/**
 * Nested under `admin` so the broad invalidation the user mutations already do
 * sweeps the catalogue too, and so the catalogue's own keys can be invalidated
 * by prefix without naming every filter combination.
 */
export const catalogKeys = {
  all: ['admin', 'catalog'] as const,
  /** When models.dev was last fetched, and whether that failed (GET /admin/catalog). */
  status: () => [...catalogKeys.all, 'status'] as const,
  models: () => [...catalogKeys.all, 'models'] as const,
  modelPage: (filters: CatalogFilters) => [...catalogKeys.models(), filters] as const,
  /** The unfiltered probe behind the empty state; see `useChannelReach`. */
  reach: () => [...catalogKeys.models(), 'reach'] as const,
}

export type CatalogFilters = {
  page: number
  limit: number
  /** Matched against model id, name and family, server-side. */
  search: string
  /** On: only models switched on here. Off: no constraint either way. */
  enabledOnly: boolean
}

export function useCatalogModels(filters: CatalogFilters) {
  return useQuery({
    queryKey: catalogKeys.modelPage(filters),
    queryFn: async ({ signal }) =>
      unwrap(
        await api.GET('/admin/catalog/models', {
          params: {
            query: {
              page: filters.page,
              limit: filters.limit,
              ...(filters.search ? { search: filters.search } : {}),
              ...(filters.enabledOnly ? { enabled: true } : {}),
            },
          },
          signal,
        }),
      ),
    // Every filter is part of the key, so without this the grid would empty out
    // and re-lay-out on each keystroke and page turn: the one mistake AGENTS.md
    // says this repo keeps making.
    placeholderData: (previous) => previous,
    // What this answers depends on the channels, and the channel mutations live
    // in another feature whose invalidation of the catalogue keys is easy to
    // lose track of. Refetching on mount costs one request and cannot replay a
    // pre-channel answer to someone who has just come back from adding one.
    staleTime: 0,
  })
}

/**
 * Whether any channel has been configured at all.
 *
 * Asked of the channel list rather than of this endpoint, because the two
 * questions have come apart: a channel whose model list has not been fetched
 * yet serves nothing, and answering "no channels" to an operator who has just
 * added one would send them to add a second.
 *
 * It shares the channel list's own key, so the answer is whatever that page
 * last read and the channel mutations already invalidate it.
 */
export function useHasChannels() {
  const { data, isPending } = useAdminChannels()
  // `undefined` while the question is still open, so the grid can wait rather
  // than flash the wrong empty state for a frame.
  return { hasChannels: data === undefined ? undefined : data.length > 0, isPending }
}

/**
 * Whether the configured channels serve any model at all.
 *
 * The grid has to tell three things apart that all arrive as an empty page:
 * nothing configured, nothing fetched yet, and nothing matching the filters.
 * The filtered query cannot answer any of them, so this asks the one unfiltered
 * question — with no search and no enabled constraint, is there anything at
 * all? `limit: 1` because only `total` is read; the page itself is thrown
 * away, and asking for fewer rows than that is not allowed.
 *
 * It sits under the `models` key prefix, so the channel-model mutations — which
 * invalidate that prefix — sweep it along with the pages.
 */
export function useChannelReach() {
  return useQuery({
    queryKey: catalogKeys.reach(),
    queryFn: async ({ signal }) =>
      unwrap(
        await api.GET('/admin/catalog/models', {
          params: { query: { page: 1, limit: 1 } },
          signal,
        }),
      ),
    select: (page) => page.total > 0,
    // Same reason as the page query, and it matters more here: this one decides
    // between "go and fetch a channel's models" and "no results", and getting
    // that wrong right after a list was fetched is the worst moment to get it
    // wrong.
    staleTime: 0,
  })
}

/**
 * A model's settings belong to the model, not to a channel: the same id served
 * through three channels is one model to the people choosing it. Saving moves
 * both the marketplace and the chat picker, which reads the levels and the
 * model list from `/models`.
 */
function useInvalidateModels() {
  const queryClient = useQueryClient()
  return () => {
    void queryClient.invalidateQueries({ queryKey: catalogKeys.models() })
    void queryClient.invalidateQueries({ queryKey: queryKeys.models.all })
  }
}

export function useUpdateModelSettings() {
  const invalidate = useInvalidateModels()
  return useMutation({
    mutationFn: async ({ modelId, override }: { modelId: string; override: ModelOverride }) =>
      unwrap(
        await api.PUT('/admin/models/{modelId}/settings', {
          params: { path: { modelId } },
          body: override,
        }),
      ),
    onSuccess: invalidate,
  })
}

export function useResetModelSettings() {
  const invalidate = useInvalidateModels()
  return useMutation({
    mutationFn: async (modelId: string) =>
      unwrap(
        await api.DELETE('/admin/models/{modelId}/settings', { params: { path: { modelId } } }),
      ),
    onSuccess: invalidate,
  })
}

/**
 * Counts a model's total spending cap again from now. Moves the marketplace
 * (the card's figure) and the chat picker (a model that was at its cap is
 * usable again at once).
 */
export function useResetModelCapacity() {
  const invalidate = useInvalidateModels()
  return useMutation({
    mutationFn: async (modelId: string) =>
      unwrap(
        await api.POST('/admin/models/{modelId}/capacity/reset', { params: { path: { modelId } } }),
      ),
    onSuccess: invalidate,
  })
}

/*
 * There is deliberately no mutation here for switching a model on.
 *
 * Offering a model is a fact about a channel's model, not about the catalogue
 * entry: the same model id can arrive through three channels, and "enable it"
 * would have to pick one of them on the operator's behalf. The channel panel
 * owns that switch — `features/admin/channels/use-channel-models.ts` — and it
 * invalidates this feature's keys when it moves, so the marketplace follows
 * along without ever writing.
 */

/** When the models.dev snapshot was fetched, how big it is, and why the last refresh failed if it did. */
export function useCatalogStatus() {
  return useQuery({
    queryKey: catalogKeys.status(),
    queryFn: async ({ signal }) => unwrap(await api.GET('/admin/catalog', { signal })),
  })
}

/**
 * Fetches models.dev now. The server also does this on its own once the
 * snapshot is a day old (it checks hourly); this is for not waiting.
 */
export function useRefreshCatalog() {
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: async () => unwrap(await api.POST('/admin/catalog', {})),
    onSuccess: (status) => {
      // The response is the new status; no need to go and ask for it again.
      queryClient.setQueryData(catalogKeys.status(), status)
      // What a card shows from models.dev (name, limits, prices) may have changed.
      void queryClient.invalidateQueries({ queryKey: catalogKeys.models() })
    },
    onError: () => {
      // A failed refresh answers 502, so there is no status in the response to
      // read - the server recorded `lastError` against the snapshot it kept.
      // Without this the button would simply stop spinning and say nothing.
      void queryClient.invalidateQueries({ queryKey: catalogKeys.status() })
    },
  })
}
