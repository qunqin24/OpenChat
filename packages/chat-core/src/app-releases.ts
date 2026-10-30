// The Android app's releases, apart from the server's: tagged android-v1.4.0
// (android-v1.4.0-rc.1 for a pre-release), each with the APK
// openchat-android-v1.4.0.apk, published by .github/workflows/android-release.yml.
// They are never GitHub's "latest" release - that stays the server's, which
// is what a server checks for its own updates - so the app reads the list of
// releases and picks the newest of its own here.

import { compareVersions, isNewer, isVersion } from './semver'

/** What every Android release tag starts with. */
export const ANDROID_TAG_PREFIX = 'android-'

/** The version an Android release tag names (android-v1.4.0 → v1.4.0), or null for any other tag. */
export function androidVersionOf(tag: string): string | null {
  if (!tag.startsWith(ANDROID_TAG_PREFIX)) return null
  const version = tag.slice(ANDROID_TAG_PREFIX.length)
  return isVersion(version) ? version : null
}

/** The APK an Android release carries, as the workflow names it: openchat-android-v1.4.0.apk. */
export function apkAssetName(version: string): string {
  return `openchat-android-v${version.replace(/^v/, '')}.apk`
}

/** The part of GitHub's release that is read. */
export interface ReleaseLike {
  tag_name?: unknown
  draft?: unknown
  prerelease?: unknown
  body?: unknown
  assets?: unknown
}

export interface AppRelease {
  /** The git tag: android-v1.4.0. */
  tag: string
  /** The version without its v: 1.4.0. */
  version: string
  /** The release notes, Markdown. */
  notes: string
  apkUrl: string
  /** The APK's size in bytes; 0 when GitHub did not say. */
  size: number
}

/** A release as one the app could install, or null: not the app's, a draft, or without an APK to fetch over https. */
export function appReleaseOf(release: ReleaseLike): AppRelease | null {
  const tag = typeof release.tag_name === 'string' ? release.tag_name : ''
  const version = androidVersionOf(tag)
  if (!version || release.draft === true) return null
  const name = apkAssetName(version)
  const assets = Array.isArray(release.assets) ? (release.assets as { name?: unknown; size?: unknown; browser_download_url?: unknown }[]) : []
  const apk = assets.find((asset) => asset?.name === name)
  // What is downloaded is installed, so only GitHub over https is followed.
  if (!apk || typeof apk.browser_download_url !== 'string' || !apk.browser_download_url.startsWith('https://')) return null
  return {
    tag,
    version: version.slice(1),
    notes: typeof release.body === 'string' ? release.body.trim() : '',
    apkUrl: apk.browser_download_url,
    size: typeof apk.size === 'number' ? apk.size : 0,
  }
}

/**
 * The newest of the app's releases that is newer than `installed`, or null.
 * Pre-releases are offered only to an installed pre-release, so the people
 * testing a release candidate get the next one and everybody else does not.
 */
export function newestAppRelease(releases: readonly ReleaseLike[], installed: string): AppRelease | null {
  const testing = installed.includes('-')
  let best: AppRelease | null = null
  for (const release of releases) {
    if (release.prerelease === true && !testing) continue
    const found = appReleaseOf(release)
    if (!found || !isNewer(found.version, installed)) continue
    if (!best || compareVersions(found.version, best.version) === 1) best = found
  }
  return best
}
