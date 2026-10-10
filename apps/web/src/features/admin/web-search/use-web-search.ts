import type { components } from '@openchat/api-client'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'

import { api, unwrap } from '@/lib/api'
import { adminKeys } from '@/features/admin/use-admin'
import { queryKeys } from '@/lib/query-keys'

type Schemas = components['schemas']

export type WebSearchSettings = Schemas['WebSearchSettings']
export type UpdateWebSearchSettings = Schemas['UpdateWebSearchSettingsRequest']
export type WebSearchCompression = Schemas['WebSearchCompression']
export type WebSearchDepth = Schemas['WebSearchDepth']
export type WebSearchTestResult = Schemas['WebSearchTestResult']

export function useWebSearchSettings() {
  return useQuery({
    queryKey: adminKeys.webSearch(),
    queryFn: async ({ signal }) => unwrap(await api.GET('/admin/web-search', { signal })),
  })
}

/** Every change answers with the whole settings, which replace the cached ones. */
export function useUpdateWebSearchSettings() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async (body: UpdateWebSearchSettings) =>
      unwrap(await api.PATCH('/admin/web-search', { body })),
    onSuccess: (next) => {
      queryClient.setQueryData(adminKeys.webSearch(), next)
      // The composer's web switch appears and disappears with these settings.
      void queryClient.invalidateQueries({ queryKey: queryKeys.features })
    },
  })
}

export function useTestWebSearch() {
  return useMutation({
    mutationFn: async (body: { query?: string; apiKey?: string }) =>
      unwrap(await api.POST('/admin/web-search/test', { body })),
  })
}
