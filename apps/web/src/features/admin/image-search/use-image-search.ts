import type { components } from '@openchat/api-client'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'

import { api, unwrap } from '@/lib/api'
import { adminKeys } from '@/features/admin/use-admin'

type Schemas = components['schemas']

export type ImageSearchSettings = Schemas['ImageSearchSettings']
export type ImageSearchProvider = Schemas['ImageSearchProvider']
export type UpdateImageSearchSettings = Schemas['UpdateImageSearchSettingsRequest']
export type ImageSearchTestResult = Schemas['ImageSearchTestResult']

export function useImageSearchSettings() {
  return useQuery({
    queryKey: adminKeys.imageSearch(),
    queryFn: async ({ signal }) => unwrap(await api.GET('/admin/image-search', { signal })),
  })
}

/** Every change answers with the whole settings, which replace the cached ones. */
export function useUpdateImageSearchSettings() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async (body: UpdateImageSearchSettings) => unwrap(await api.PATCH('/admin/image-search', { body })),
    onSuccess: (next) => queryClient.setQueryData(adminKeys.imageSearch(), next),
  })
}

export function useTestImageSearch() {
  return useMutation({
    mutationFn: async (body: Schemas['ImageSearchTestRequest']) => unwrap(await api.POST('/admin/image-search/test', { body })),
  })
}
