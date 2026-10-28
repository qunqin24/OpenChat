import type { components } from '@openchat/api-client'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'

import { adminKeys } from '@/features/admin/use-admin'
import { api, unwrap } from '@/lib/api'
import { queryKeys } from '@/lib/query-keys'

type Schemas = components['schemas']

export type PasskeySettings = Schemas['PasskeySettings']
export type UpdatePasskeySettings = Schemas['UpdatePasskeySettingsRequest']

export function usePasskeySettings() {
  return useQuery({
    queryKey: adminKeys.passkeys(),
    queryFn: async ({ signal }) => unwrap(await api.GET('/admin/authentication/passkeys', { signal })),
  })
}

/** Every change answers with the whole settings, which replace the cached ones. */
export function useUpdatePasskeySettings() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async (body: UpdatePasskeySettings) => unwrap(await api.PATCH('/admin/authentication/passkeys', { body })),
    onSuccess: (next) => {
      queryClient.setQueryData(adminKeys.passkeys(), next)
      // The sign-in page's passkey button and the account settings follow the switch.
      void queryClient.invalidateQueries({ queryKey: queryKeys.auth.options })
    },
  })
}
