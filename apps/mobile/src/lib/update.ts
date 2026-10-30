import AsyncStorage from '@react-native-async-storage/async-storage'
import * as Application from 'expo-application'
import { cacheDirectory, createDownloadResumable, deleteAsync, getContentUriAsync, getInfoAsync, makeDirectoryAsync, type DownloadResumable } from 'expo-file-system/legacy'
import { ResultCode, startActivityAsync } from 'expo-intent-launcher'
import { useEffect } from 'react'
import { AppState, Platform } from 'react-native'
import { create } from 'zustand'

import { ANDROID_TAG_PREFIX, apkAssetName, newestAppRelease, type AppRelease, type ReleaseLike } from '@openchat/chat-core/app-releases'

import { releasesRepo } from '@/config'

// Updates for the Android app, from the GitHub releases of `releasesRepo`: the
// app has releases of its own, tagged android-v1.4.0 apart from the server's,
// each with openchat-android-v1.4.0.apk (chat-core/app-releases.ts). The app
// looks for a newer one when it starts and when it comes back to the front
// (at most every six hours, GitHub allows sixty unauthenticated calls an hour
// per address), downloads the APK and hands it to the system installer, which
// accepts it only if it is signed with the same key as the installed app.

/** How long a look is trusted: the app does not ask again sooner on its own. */
const CHECK_EVERY_MS = 6 * 60 * 60 * 1000
const REQUEST_TIMEOUT_MS = 10_000
/** Intent.FLAG_GRANT_READ_URI_PERMISSION: the installer may read the file we name. */
const GRANT_READ_URI_PERMISSION = 1
const APK_TYPE = 'application/vnd.android.package-archive'

const CHECKED_AT = 'openchat.update.checkedAt'
const RELEASES = 'openchat.update.releases'
/** Enough recent releases to reach the app's newest among the server's. */
const RELEASES_PER_PAGE = 30
const SKIPPED = 'openchat.update.skipped'

/** A release the app could install. */
export type AvailableUpdate = AppRelease

export type CheckResult = { status: 'latest' } | { status: 'available'; update: AvailableUpdate } | { status: 'failed' }

/** Whether this platform installs updates itself; elsewhere the checks do nothing. */
export const updatesSupported = Platform.OS === 'android'

type Phase = 'idle' | 'downloading' | 'failed' | 'permission'

interface UpdateStore {
  /** The update the prompt offers; null when there is nothing to offer. */
  update: AvailableUpdate | null
  phase: Phase
  /** 0-100, while downloading. */
  percent: number
}

const useStore = create<UpdateStore>(() => ({ update: null, phase: 'idle', percent: 0 }))

/** Versions the reader said "later" to, until the app is started again. */
const postponed = new Set<string>()
let download: DownloadResumable | null = null

async function read(key: string): Promise<string | null> {
  try {
    return await AsyncStorage.getItem(key)
  } catch {
    return null
  }
}

async function write(key: string, value: string): Promise<void> {
  try {
    await AsyncStorage.setItem(key, value)
  } catch {
    // Nothing here is needed for the app to work; the next check asks again.
  }
}

interface Cached {
  etag: string
  /** Only the app's releases, and only what is read of them. */
  releases: ReleaseLike[]
}

function parseCached(raw: string | null): Cached | null {
  if (!raw) return null
  try {
    const value = JSON.parse(raw) as Cached
    return typeof value.etag === 'string' && Array.isArray(value.releases) ? value : null
  } catch {
    return null
  }
}

/** What of a release is kept: the app's, with the fields chat-core reads. */
function keep(releases: unknown): ReleaseLike[] {
  if (!Array.isArray(releases)) return []
  return (releases as ReleaseLike[])
    .filter((r) => typeof r?.tag_name === 'string' && r.tag_name.startsWith(ANDROID_TAG_PREFIX))
    .map(({ tag_name, draft, prerelease, body, assets }) => ({ tag_name, draft, prerelease, body, assets }))
}

type Look = { kind: 'releases'; releases: ReleaseLike[] } | { kind: 'failed' }

/** The repository's recent releases, from the cache when GitHub says they have not changed. */
async function look(): Promise<Look> {
  const cached = parseCached(await read(RELEASES))
  const controller = new AbortController()
  const timer = setTimeout(() => controller.abort(), REQUEST_TIMEOUT_MS)
  try {
    const response = await fetch(`https://api.github.com/repos/${releasesRepo}/releases?per_page=${RELEASES_PER_PAGE}`, {
      headers: { Accept: 'application/vnd.github+json', ...(cached ? { 'If-None-Match': cached.etag } : {}) },
      signal: controller.signal,
    })
    // Only a real answer counts as a look; no connection is tried again soon.
    await write(CHECKED_AT, String(Date.now()))
    if (response.status === 304 && cached) return { kind: 'releases', releases: cached.releases }
    if (!response.ok) return { kind: 'failed' }
    const releases = keep(await response.json())
    const etag = response.headers.get('etag')
    if (etag) await write(RELEASES, JSON.stringify({ etag, releases } satisfies Cached))
    return { kind: 'releases', releases }
  } catch {
    return { kind: 'failed' }
  } finally {
    clearTimeout(timer)
  }
}

let looking: Promise<CheckResult> | null = null

/**
 * Looks for a newer release. On its own (not `manual`) it asks at most every
 * six hours, says nothing when it fails, and leaves out a version the reader
 * skipped or postponed. A manual check always asks and always reports; when it
 * finds an update it opens the prompt, whatever was skipped.
 */
export function checkForUpdates({ manual = false }: { manual?: boolean } = {}): Promise<CheckResult> {
  if (!updatesSupported) return Promise.resolve({ status: 'latest' })
  if (!manual && looking) return looking
  const run = check(manual).finally(() => {
    if (looking === run) looking = null
  })
  if (!manual) looking = run
  return run
}

async function check(manual: boolean): Promise<CheckResult> {
  if (!manual) {
    const checkedAt = Number(await read(CHECKED_AT))
    if (checkedAt > 0 && Date.now() - checkedAt < CHECK_EVERY_MS) return { status: 'latest' }
  }
  const found = await look()
  if (found.kind === 'failed') return { status: 'failed' }
  const installed = Application.nativeApplicationVersion
  const update = installed ? newestAppRelease(found.releases, installed) : null
  if (!update) return { status: 'latest' }
  if (!manual && ((await read(SKIPPED)) === update.tag || postponed.has(update.tag))) return { status: 'latest' }
  // Do not interrupt a download or an install in progress with the same offer.
  const { phase } = useStore.getState()
  if (phase === 'idle' || phase === 'failed') useStore.setState({ update, phase: 'idle', percent: 0 })
  return { status: 'available', update }
}

/** Closes the prompt for now; the same version is offered again the next time the app starts. */
export function postpone(): void {
  const { update } = useStore.getState()
  void download?.cancelAsync().catch(() => undefined)
  download = null
  if (update) postponed.add(update.tag)
  useStore.setState({ update: null, phase: 'idle', percent: 0 })
}

/** Closes the prompt and does not offer this version again on its own. */
export function skip(): void {
  const { update } = useStore.getState()
  if (update) void write(SKIPPED, update.tag)
  postpone()
}

/** The downloaded APK for `update`, fetched unless it is already there whole. */
async function fetchApk(update: AvailableUpdate): Promise<string> {
  if (!cacheDirectory) throw new Error('no cache directory')
  const dir = `${cacheDirectory}update/`
  const file = `${dir}${apkAssetName(update.version)}`
  const have = await getInfoAsync(file)
  if (have.exists && update.size > 0 && have.size === update.size) return file
  // Older downloads are of no use any more.
  await deleteAsync(dir, { idempotent: true })
  await makeDirectoryAsync(dir, { intermediates: true })
  useStore.setState({ percent: 0 })
  const task = createDownloadResumable(update.apkUrl, file, {}, ({ totalBytesWritten, totalBytesExpectedToWrite }) => {
    // The size GitHub reports is known up front; the response may not say.
    const total = totalBytesExpectedToWrite > 0 ? totalBytesExpectedToWrite : update.size
    if (total > 0) useStore.setState({ percent: Math.min(99, Math.floor((totalBytesWritten / total) * 100)) })
  })
  download = task
  const result = await task.downloadAsync()
  if (download === task) download = null
  // Undefined: the reader cancelled.
  if (!result) throw new Error('cancelled')
  if (result.status !== 200) throw new Error(`download answered ${result.status}`)
  const info = await getInfoAsync(file)
  if (!info.exists || (update.size > 0 && info.size !== update.size)) throw new Error('download is incomplete')
  return file
}

/** Downloads the offered update and opens the system installer on it. */
export async function installUpdate(): Promise<void> {
  const { update, phase } = useStore.getState()
  if (!update || phase === 'downloading') return
  useStore.setState({ phase: 'downloading', percent: 0 })
  try {
    const file = await fetchApk(update)
    // The prompt may have been closed while the file came down.
    if (useStore.getState().update !== update) return
    const result = await startActivityAsync('android.intent.action.VIEW', {
      data: await getContentUriAsync(file),
      flags: GRANT_READ_URI_PERMISSION,
      type: APK_TYPE,
    })
    // When the install goes through the app is replaced and never gets here.
    // Coming back means the reader declined, or went to let this app install
    // apps (the installer sends them to that setting first): say so, and the
    // file is still there for a second try.
    if (useStore.getState().update === update) {
      useStore.setState({ phase: result.resultCode === ResultCode.Success ? 'idle' : 'permission' })
    }
  } catch {
    if (useStore.getState().update === update) useStore.setState({ phase: 'failed' })
  }
}

/**
 * The update the prompt offers, with what the prompt does about it. Starts the
 * automatic checks, so call it once, where the app starts (UpdatePrompt does).
 */
export function useUpdateCheck() {
  const state = useStore()
  useEffect(() => {
    if (!updatesSupported || __DEV__) return
    void checkForUpdates()
    // An app left open for days looks again when it comes back to the front.
    const subscription = AppState.addEventListener('change', (next) => {
      if (next === 'active') void checkForUpdates()
    })
    return () => subscription.remove()
  }, [])
  return { ...state, install: installUpdate, postpone, skip }
}
