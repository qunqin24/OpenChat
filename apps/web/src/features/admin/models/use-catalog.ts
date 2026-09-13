import type { components } from '@openchat/api-client'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'

import { api, unwrap } from '@/lib/api'

type Schemas = components['schemas']

export type CatalogStatus = Schemas['CatalogStatus']
export type CatalogProvider = Schemas['CatalogProvider']
export type CatalogModel = Schemas['CatalogModel']

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
            },
          },
          signal,
        }),
      ),
    // Every filter is part of the key, so without this the grid would empty out
    // and re-lay-out on each keystroke, provider change and page turn — the one
    // mistake AGENTS.md says this repo keeps making.
    placeholderData: (previous) => previous,
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

/**
 * Switching one model on or off. Invalidating by prefix refreshes whichever
 * pages are cached; because `placeholderData` holds the current page on screen
 * while that happens, the grid does not blink.
 */
export function useUpdateCatalogModel() {
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: async ({
      providerId,
      modelId,
      enabled,
    }: {
      providerId: string
      modelId: string
      enabled: boolean
    }) =>
      unwrap(
        await api.PATCH('/admin/catalog/models/{providerId}/{modelId}', {
          params: { path: { providerId, modelId } },
          body: { enabled },
        }),
      ),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: catalogKeys.models() })
    },
  })
}
