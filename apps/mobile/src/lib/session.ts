import { useMutation, useQuery } from '@tanstack/react-query'
import { Passkey, type PasskeyGetRequest } from 'react-native-passkey'

import { isOpenChatError, type components } from '@openchat/api-client'
import { accountDisabledOf, type AccountDisabled } from '@openchat/chat-core/account-disabled'
import {
  adminOf,
  AuthFieldError,
  toAuthError,
  type LoginInput,
  type RegisterInput,
  type RegisterOutcome,
  type SessionUser,
} from '@openchat/chat-core/auth'
import { queryKeys } from '@openchat/chat-core/query-keys'

import { api, asApp, unwrap } from './api'
import { queryClient } from './query'
import { resetChatStore } from './store'
import { forgetToken, loadToken, saveToken } from './token'

type SignInResult = components['schemas']['SignInResult']

/**
 * Where the app stands with the server. A disabled account keeps its session
 * (the server says why on every request), so it is a state of its own rather
 * than signed out.
 */
export type Session =
  | { state: 'signed-out' }
  | { state: 'signed-in'; user: SessionUser }
  | { state: 'disabled'; details: AccountDisabled }

/**
 * Asks who the token belongs to. Asking also renews an app's session once it
 * is past half its life, so an app opened within any 30 days stays signed in.
 * Thrown only when the server could not be asked: an offline start keeps the
 * token and offers to try again.
 */
async function fetchSession(): Promise<Session> {
  if (!(await loadToken())) return { state: 'signed-out' }
  const result = await api.GET('/auth/session')
  if (result.response.status === 401) {
    await forgetToken()
    return { state: 'signed-out' }
  }
  try {
    return { state: 'signed-in', user: unwrap(result) as SessionUser }
  } catch (error) {
    const details = accountDisabledOf(error)
    if (details) return { state: 'disabled', details }
    throw error
  }
}

export function useSession() {
  return useQuery({ queryKey: queryKeys.auth.session, queryFn: fetchSession, staleTime: Infinity, retry: false })
}

/**
 * The signed-in user, for screens inside the (app) guard. Null for the moment
 * such a screen outlives the session (signing out re-renders it before the
 * guard takes it away): render nothing then.
 */
export function useUser(): SessionUser | null {
  const { data } = useSession()
  return data?.state === 'signed-in' ? data.user : null
}

/**
 * Everything another account must not see goes, and the new session is set.
 * Not `queryClient.clear()`: that also drops the session query the root
 * layout is watching, and the session set after it would never reach it.
 */
function settle(session: Session) {
  resetChatStore()
  queryClient.setQueryData(queryKeys.auth.session, session)
  queryClient.removeQueries({ predicate: (query) => !isSessionKey(query.queryKey) })
}

const isSessionKey = (key: readonly unknown[]) =>
  key.length === queryKeys.auth.session.length && key.every((part, i) => part === queryKeys.auth.session[i])

async function signedIn(result: SignInResult): Promise<SessionUser> {
  if (!result.token) throw new AuthFieldError('form', 'auth.error.unknown')
  await saveToken(result.token)
  const { token: _token, expiresAt: _expiresAt, ...user } = result
  settle({ state: 'signed-in', user })
  return user
}

export async function signOut() {
  // Ends the session on the server too; a failure there (offline) still
  // signs this device out, and the token lapses on its own.
  await api.POST('/auth/logout').catch(() => undefined)
  await forgetToken()
  settle({ state: 'signed-out' })
}

/** The server forgot the token: this device is signed out, nothing to call. */
export async function sessionEnded() {
  await forgetToken()
  settle({ state: 'signed-out' })
}

/** Someone's request was refused because the account is disabled. */
export function showIfAccountDisabled(error: unknown) {
  const details = accountDisabledOf(error)
  if (!details) return
  const session: Session = { state: 'disabled', details }
  queryClient.setQueryData(queryKeys.auth.session, session)
}

export function useAuthOptions() {
  return useQuery({
    queryKey: queryKeys.auth.options,
    queryFn: async () => unwrap(await api.GET('/auth/options')),
  })
}

export function useLogin() {
  return useMutation({
    mutationFn: async (input: LoginInput) => {
      const { data, error } = await api.POST('/auth/login', { body: input, params: asApp })
      if (error) throw toAuthError(error)
      return signedIn(data)
    },
  })
}

export function useRegister() {
  return useMutation({
    mutationFn: async (input: RegisterInput): Promise<RegisterOutcome> => {
      const { data, error, response } = await api.POST('/auth/register', { body: input, params: asApp })
      if (error) throw toAuthError(error)
      // 202: created, but an administrator has to approve it first. No session.
      if (response.status === 202) {
        return { status: 'pending', admin: adminOf((data as { admin?: unknown } | undefined)?.admin) }
      }
      return { status: 'signed_in', user: await signedIn(data as SignInResult) }
    },
  })
}

/** Signs in with a passkey through Credential Manager; null when the person backed out. */
export function usePasskeyLogin() {
  return useMutation({
    mutationFn: async (): Promise<SessionUser | null> => {
      const begun = await api.POST('/auth/passkey/options')
      if (begun.error || !begun.data) throw toAuthError(begun.error)
      let credential
      try {
        credential = await Passkey.get(withoutNulls(begun.data.publicKey) as unknown as PasskeyGetRequest)
      } catch (error) {
        const name = (error as { error?: string } | undefined)?.error
        if (name === 'UserCancelled' || name === 'NoCredentials' || name === 'Interrupted') return null
        throw new AuthFieldError(
          'form',
          name === 'NotSupported' ? 'auth.error.device-passkey-unsupported' : 'auth.error.invalid-passkey',
        )
      }
      const { data, error } = await api.POST('/auth/passkey', {
        body: { challengeId: begun.data.challengeId, credential: credential as unknown as Record<string, unknown> },
        params: asApp,
      })
      if (error) throw toAuthError(error)
      return signedIn(data)
    },
  })
}

/**
 * Credential Manager rejects the request outright on a null where it expects
 * a value or nothing (go-webauthn sends `"allowCredentials": null` and the
 * like), so nulls go before the options are handed over.
 */
export function withoutNulls(value: unknown): unknown {
  if (Array.isArray(value)) return value.map(withoutNulls)
  if (value && typeof value === 'object') {
    return Object.fromEntries(
      Object.entries(value)
        .filter(([, v]) => v !== null)
        .map(([k, v]) => [k, withoutNulls(v)]),
    )
  }
  return value
}

/** A failure a form shows: the server's reason, or that it could not be reached. */
export function failureKey(error: unknown) {
  if (error instanceof AuthFieldError) return error.key
  if (isOpenChatError(error)) return 'auth.error.unknown' as const
  return 'mobile.network' as const
}
