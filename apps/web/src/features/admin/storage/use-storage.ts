import type { components } from '@openchat/api-client'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'

import { api, unwrap } from '@/lib/api'
import { adminKeys } from '@/features/admin/use-admin'

type Schemas = components['schemas']

export type StorageSettings = Schemas['StorageSettings']
export type UpdateStorageSettings = Schemas['UpdateStorageSettingsRequest']
export type StorageTestResult = Schemas['StorageTestResult']

export function useStorageSettings() {
  return useQuery({
    queryKey: adminKeys.storage(),
    queryFn: async ({ signal }) => unwrap(await api.GET('/admin/storage', { signal })),
  })
}

/** Every change answers with the whole settings, which replace the cached ones. */
export function useUpdateStorageSettings() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async (body: UpdateStorageSettings) =>
      unwrap(await api.PATCH('/admin/storage', { body })),
    onSuccess: (next) => {
      queryClient.setQueryData(adminKeys.storage(), next)
    },
  })
}

/** Tries a configuration without saving it. */
export function useTestStorage() {
  return useMutation({
    mutationFn: async (body: UpdateStorageSettings) =>
      unwrap(await api.POST('/admin/storage/test', { body })),
  })
}
