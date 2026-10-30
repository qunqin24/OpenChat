import type { components } from '@openchat/api-client'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { ImageManipulator, SaveFormat } from 'expo-image-manipulator'
import { FileSystemUploadType, uploadAsync } from 'expo-file-system/legacy'
import { Platform } from 'react-native'
import { Passkey as Passkeys, type PasskeyCreateRequest } from 'react-native-passkey'

import { toAuthError, type SessionUser } from '@openchat/chat-core/auth'
import { queryKeys } from '@openchat/chat-core/query-keys'

import { serverUrl } from '@/config'
import { api, unwrap } from '@/lib/api'
import { withoutNulls, type Session } from '@/lib/session'
import { currentToken } from '@/lib/token'

export type Passkey = components['schemas']['Passkey']

/** The side of the picture that is sent, in pixels (the web's lib/avatar.ts). */
const AVATAR_SIDE = 256

/** The new account details, everywhere the session is shown. */
function useSetUser() {
  const queryClient = useQueryClient()
  return (user: SessionUser) => {
    const session: Session = { state: 'signed-in', user }
    queryClient.setQueryData(queryKeys.auth.session, session)
  }
}

/** Name and email, through the self-service endpoint that cannot reach the role. */
export function useUpdateProfile() {
  const setUser = useSetUser()
  return useMutation({
    meta: { silent: true },
    mutationFn: async (body: { name?: string; email?: string }) => {
      const { data, error } = await api.PATCH('/auth/me', { body })
      if (error) throw toAuthError(error)
      return data as SessionUser
    },
    onSuccess: setUser,
  })
}

/** Signs out the account's other sessions; this one stays (the server keeps the caller's). */
export function useChangePassword() {
  return useMutation({
    meta: { silent: true },
    mutationFn: async (body: { currentPassword: string; newPassword: string }) => {
      const { error } = await api.POST('/auth/password', { body })
      if (error) throw toAuthError(error)
    },
  })
}

/** Why a picture was not set, as the `settings.account.avatar-*` key that says so. */
export type AvatarError = 'type' | 'too-large' | 'failed'

export class AvatarFailed extends Error {
  constructor(readonly reason: AvatarError) {
    super(reason)
  }
}

/**
 * Sets the account's picture from one on the device - already cut to a square
 * by the system's cropper - or removes it with `null`. It is scaled down here,
 * as the web does, so a phone photo of several megabytes leaves as a few tens
 * of kilobytes.
 */
export function useSetAvatar() {
  const setUser = useSetUser()
  return useMutation({
    meta: { silent: true },
    mutationFn: async (uri: string | null): Promise<SessionUser> => {
      if (uri === null) return unwrap(await api.DELETE('/me/avatar')) as SessionUser
      let picture
      try {
        const context = ImageManipulator.manipulate(uri)
        context.resize({ width: AVATAR_SIDE, height: AVATAR_SIDE })
        picture = await (await context.renderAsync()).saveAsync({ format: SaveFormat.PNG })
      } catch {
        throw new AvatarFailed('failed')
      }
      // The picture is the body, which the typed client has no way to say.
      const response = await uploadAsync(`${serverUrl}/api/v1/me/avatar`, picture.uri, {
        httpMethod: 'PUT',
        uploadType: FileSystemUploadType.BINARY_CONTENT,
        headers: { 'Content-Type': 'image/png', Authorization: `Bearer ${currentToken() ?? ''}` },
      })
      if (response.status === 413) throw new AvatarFailed('too-large')
      if (response.status === 415) throw new AvatarFailed('type')
      if (response.status < 200 || response.status >= 300) throw new AvatarFailed('failed')
      return JSON.parse(response.body) as SessionUser
    },
    onSuccess: setUser,
  })
}

/** The signed-in account's passkeys. */
export function usePasskeys(enabled: boolean) {
  return useQuery({
    queryKey: queryKeys.auth.passkeys,
    queryFn: async ({ signal }) => unwrap(await api.GET('/auth/passkeys', { signal })).items,
    enabled,
  })
}

/** What a passkey made here is first called, as the web names one by browser and system. */
function deviceName(): string {
  const constants = Platform.constants as { Brand?: string; Model?: string }
  const model = Platform.OS === 'android' ? constants.Model : undefined
  return ['OpenChat', model].filter(Boolean).join(' · ')
}

/**
 * Adds a passkey: the server's challenge, Credential Manager's prompt to make
 * one, the answer. Resolves to null when the person backed out; throws
 * `passkey_exists` when the provider already holds one of this account's.
 */
export function useAddPasskey() {
  const queryClient = useQueryClient()
  return useMutation({
    meta: { silent: true },
    mutationFn: async (): Promise<Passkey | null> => {
      const begun = unwrap(await api.POST('/auth/passkeys/options'))
      let credential
      try {
        credential = await Passkeys.create(withoutNulls(begun.publicKey) as unknown as PasskeyCreateRequest)
      } catch (error) {
        const name = (error as { error?: string } | undefined)?.error
        if (name === 'UserCancelled' || name === 'Interrupted') return null
        if (name === 'CredentialAlreadyExists') throw new Error('passkey_exists', { cause: error })
        throw error
      }
      return unwrap(
        await api.POST('/auth/passkeys', {
          body: { challengeId: begun.challengeId, credential: credential as unknown as Record<string, unknown>, name: deviceName() },
        }),
      )
    },
    onSuccess: () => void queryClient.invalidateQueries({ queryKey: queryKeys.auth.passkeys }),
  })
}

export function useRenamePasskey() {
  const queryClient = useQueryClient()
  return useMutation({
    meta: { silent: true },
    mutationFn: async ({ id, name }: { id: string; name: string }) =>
      unwrap(await api.PATCH('/auth/passkeys/{passkeyId}', { params: { path: { passkeyId: id } }, body: { name } })),
    onSuccess: () => void queryClient.invalidateQueries({ queryKey: queryKeys.auth.passkeys }),
  })
}

export function useDeletePasskey() {
  const queryClient = useQueryClient()
  return useMutation({
    meta: { silent: true },
    mutationFn: async (id: string) => unwrap(await api.DELETE('/auth/passkeys/{passkeyId}', { params: { path: { passkeyId: id } } })),
    onSuccess: () => void queryClient.invalidateQueries({ queryKey: queryKeys.auth.passkeys }),
  })
}
