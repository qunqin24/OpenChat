import type { components } from '@openchat/api-client'
import { useQuery } from '@tanstack/react-query'

import { api, unwrap } from '@/lib/api'
import { catalogKeys } from '@/features/admin/models/use-catalog'

export type CatalogProvider = components['schemas']['CatalogProvider']

/**
 * Every provider models.dev knows about: an id, a display name, how many models
 * it serves, and — where upstream documents them — a documentation URL and the
 * environment variables its key is conventionally read from.
 *
 * The whole list comes in one response and there is no filter in the key, so
 * there is nothing here for `placeholderData` to hold on to. The picker filters
 * client-side.
 *
 * The key is imported from the models feature rather than spelled out again:
 * the channel form and the model browser then share one cache entry, and a
 * refresh over there invalidates what this form is showing. Two copies of a
 * cache key fail silently when they drift, which is the worst way to fail.
 */
export function useProviderCatalog() {
  return useQuery({
    queryKey: catalogKeys.providers(),
    queryFn: async ({ signal }) => unwrap(await api.GET('/admin/catalog/providers', { signal })),
  })
}
