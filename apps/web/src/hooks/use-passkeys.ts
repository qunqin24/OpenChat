import type { components } from '@openchat/api-client'
import { startRegistration } from '@simplewebauthn/browser'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'

import { api, unwrap } from '@/lib/api'
import { passkeyName } from '@/lib/passkey-name'
import { queryKeys } from '@openchat/chat-core/query-keys'

export type Passkey = components['schemas']['Passkey']

/** The signed-in account's passkeys. */
export function usePasskeys(enabled: boolean) {
  return useQuery({
    queryKey: queryKeys.auth.passkeys,
    queryFn: async ({ signal }) => unwrap(await api.GET('/auth/passkeys', { signal })).items,
    enabled,
  })
}

/**
 * Adds a passkey: the server's challenge, the browser's prompt to make one,
 * the answer. Resolves to null when the person closed the prompt.
 */
export function useAddPasskey() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async (): Promise<Passkey | null> => {
      const begun = unwrap(await api.POST('/auth/passkeys/options'))
      let credential
      try {
        credential = await startRegistration({ optionsJSON: begun.publicKey as never })
      } catch (error) {
        const name = (error as { cause?: Error; name?: string }).cause?.name ?? (error as Error).name
        if (name === 'NotAllowedError' || name === 'AbortError') return null
        // The authenticator already holds one of this account's passkeys.
        if (name === 'InvalidStateError') throw new Error('passkey_exists', { cause: error })
        throw error
      }
      const name = passkeyName(navigator.userAgent)
      return unwrap(
        await api.POST('/auth/passkeys', {
          body: { challengeId: begun.challengeId, credential: credential as unknown as Record<string, unknown>, ...(name ? { name } : {}) },
        }),
      )
    },
    onSuccess: () => void queryClient.invalidateQueries({ queryKey: queryKeys.auth.passkeys }),
  })
}

export function useRenamePasskey() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async ({ id, name }: { id: string; name: string }) =>
      unwrap(await api.PATCH('/auth/passkeys/{passkeyId}', { params: { path: { passkeyId: id } }, body: { name } })),
    onSuccess: () => void queryClient.invalidateQueries({ queryKey: queryKeys.auth.passkeys }),
  })
}

export function useDeletePasskey() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async (id: string) => unwrap(await api.DELETE('/auth/passkeys/{passkeyId}', { params: { path: { passkeyId: id } } })),
    onSuccess: () => void queryClient.invalidateQueries({ queryKey: queryKeys.auth.passkeys }),
  })
}
