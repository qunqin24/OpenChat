import * as SecureStore from 'expo-secure-store'

// The session token, kept in the Android Keystore through SecureStore and in
// memory for the request middleware (which cannot wait on the store). It is
// the session itself: whoever holds it is signed in until it is signed out.

const KEY = 'openchat.session'

let current: string | null = null

/** Reads the stored token once, before the first request. */
export async function loadToken(): Promise<string | null> {
  current = await SecureStore.getItemAsync(KEY)
  return current
}

export function currentToken(): string | null {
  return current
}

export async function saveToken(token: string): Promise<void> {
  current = token
  await SecureStore.setItemAsync(KEY, token)
}

export async function forgetToken(): Promise<void> {
  current = null
  await SecureStore.deleteItemAsync(KEY)
}
