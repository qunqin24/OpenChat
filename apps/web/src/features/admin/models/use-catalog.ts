import type { components, paths } from '@openchat/api-client'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'

import { api, unwrap } from '@/lib/api'
import { useAdminChannels } from '@/features/admin/use-admin'

type Schemas = components['schemas']

export type CatalogStatus = Schemas['CatalogStatus']
export type CatalogProvider = Schemas['CatalogProvider']
export type CatalogModel = Schemas['CatalogModel']
export type ModelChannel = Schemas['ModelChannel']

/**
 * Which catalogue the page is looking at. Read off the contract rather than
 * spelled out here, so adding a scope server-side surfaces as a type error in
 * the switch that renders them instead of as a value nothing handles.
 *
 * `channels` is what an operator means by "our models" — only what a configured
 * channel can actually reach. `all` is the whole upstream catalogue, which is
 * where you go to decide which channel to add next.
 */
export type CatalogScope = NonNullable<
  NonNullable<paths['/admin/catalog/models']['get']['parameters']['query']>['scope']
>

/**
 * In tab order, narrowest first. `satisfies` pins it to the contract in both
 * directions: a scope that goes away stops compiling here, and one that arrives
 * has to be given a tab before the label map will type-check.
 */
export const SCOPES = ['channels', 'all'] as const satisfies readonly CatalogScope[]

export const DEFAULT_SCOPE: CatalogScope = SCOPES[0]

/**
 * Nested under `admin` so the broad invalidation the user mutations already do
 * sweeps the catalogue too, and so the catalogue's own keys can be invalidated
 * by prefix without naming every filter combination.
 */
export const catalogKeys = {
  all: ['admin', 'catalog'] as const,
  status: () => [...catalogKeys.all, 'status'] as const,
  providers: () => [...catalogKeys.all, 'providers'] as const,
  models: () => [...catalogKeys.all, 'models'] as const,
  modelPage: (filters: CatalogFilters) => [...catalogKeys.models(), filters] as const,
  /** The unfiltered channel-scope probe; see `useChannelReach`. */
  reach: () => [...catalogKeys.models(), 'reach'] as const,
}

export type CatalogFilters = {
  page: number
  limit: number
  /** Matched against model id, name and family, server-side. */
  search: string
  /** Empty means every provider. */
  providerId: string
  /** On: only models switched on here. Off: no constraint either way. */
  enabledOnly: boolean
  /** Which catalogue is being listed — see `CatalogScope`. */
  scope: CatalogScope
}

export function useCatalogStatus() {
  return useQuery({
    queryKey: catalogKeys.status(),
    queryFn: async ({ signal }) => unwrap(await api.GET('/admin/catalog', { signal })),
  })
}

export function useCatalogProviders() {
  return useQuery({
    queryKey: catalogKeys.providers(),
    queryFn: async ({ signal }) => unwrap(await api.GET('/admin/catalog/providers', { signal })),
  })
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
              ...(filters.providerId ? { providerId: filters.providerId } : {}),
              ...(filters.enabledOnly ? { enabled: true } : {}),
              scope: filters.scope,
            },
          },
          signal,
        }),
      ),
    // Every filter is part of the key — the scope included — so without this the
    // grid would empty out and re-lay-out on each keystroke, provider change,
    // page turn and scope switch: the one mistake AGENTS.md says this repo keeps
    // making.
    placeholderData: (previous) => previous,
    // What this answers depends on the channels, and the channel mutations live
    // in another feature that invalidates its own keys and not the catalogue's.
    // The global 30s would therefore replay a pre-channel answer to someone who
    // has just come back from adding one — the empty state would send them off
    // to add a second. Refetching on mount costs one request and cannot lie.
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
 * question — with no search, no provider and no enabled constraint, does the
 * channel scope return anything? `limit: 1` because only `total` is read; the
 * page itself is thrown away, and asking for fewer rows than that is not
 * allowed.
 *
 * It sits under the `models` key prefix, so a catalogue refresh — and the
 * channel-model mutations, which invalidate that prefix — sweep it along with
 * the pages.
 */
export function useChannelReach() {
  return useQuery({
    queryKey: catalogKeys.reach(),
    queryFn: async ({ signal }) =>
      unwrap(
        await api.GET('/admin/catalog/models', {
          params: { query: { page: 1, limit: 1, scope: 'channels' } },
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
 * The catalogue is fetched from models.dev on demand rather than on a timer, so
 * this is the only thing that moves it. A failed refresh keeps the previous
 * snapshot — the response says so in `lastError`, which the toolbar surfaces.
 */
export function useRefreshCatalog() {
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: async () => unwrap(await api.POST('/admin/catalog', {})),
    onSuccess: (status) => {
      // The response is the new status; no need to go and ask for it again.
      queryClient.setQueryData(catalogKeys.status(), status)
      void queryClient.invalidateQueries({ queryKey: catalogKeys.providers() })
      void queryClient.invalidateQueries({ queryKey: catalogKeys.models() })
    },
    onError: () => {
      // A failed refresh answers 502, so there is no status in the response to
      // read — the server recorded `lastError` against the snapshot it kept.
      // Without this the button would simply stop spinning and say nothing.
      void queryClient.invalidateQueries({ queryKey: catalogKeys.status() })
    },
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
