import Constants from 'expo-constants'

// What app.config.ts settled at build time; see the notes there.
const extra = (Constants.expoConfig?.extra ?? {}) as { serverUrl?: string; releasesRepo?: string }

/** The server, without a trailing slash: https://openchat.qunqin.org. */
export const serverUrl = (extra.serverUrl ?? 'https://openchat.qunqin.org').replace(/\/+$/, '')

/** The GitHub repository whose releases carry the app's updates. */
export const releasesRepo = extra.releasesRepo ?? 'qunqin24/OpenChat'
