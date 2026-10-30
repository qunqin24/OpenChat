import { describe, expect, it } from 'vitest'

import { androidVersionOf, apkAssetName, newestAppRelease, type ReleaseLike } from './app-releases'

const apk = (version: string) => ({
  name: apkAssetName(version),
  size: 1234,
  browser_download_url: `https://github.com/o/r/releases/download/android-${version}/${apkAssetName(version)}`,
})

const release = (tag: string, over: Partial<ReleaseLike> = {}): ReleaseLike => {
  const version = tag.replace(/^android-/, '')
  return { tag_name: tag, draft: false, prerelease: version.includes('-'), body: ` notes for ${tag} `, assets: [apk(version)], ...over }
}

describe('androidVersionOf', () => {
  it('reads the version from an Android tag only', () => {
    expect(androidVersionOf('android-v1.4.0')).toBe('v1.4.0')
    expect(androidVersionOf('android-v1.4.0-rc.1')).toBe('v1.4.0-rc.1')
    expect(androidVersionOf('v1.4.0')).toBeNull()
    expect(androidVersionOf('android-latest')).toBeNull()
  })
})

describe('apkAssetName', () => {
  it('names the APK by the version, with or without its v', () => {
    expect(apkAssetName('v1.4.0')).toBe('openchat-android-v1.4.0.apk')
    expect(apkAssetName('1.4.0')).toBe('openchat-android-v1.4.0.apk')
  })
})

describe('newestAppRelease', () => {
  it('picks the newest Android release, passing over the server releases among them', () => {
    const list = [release('android-v1.4.1'), { tag_name: 'v1.5.0', assets: [] }, release('android-v1.5.0'), release('android-v1.3.0')]
    const found = newestAppRelease(list, '1.4.0')
    expect(found).toMatchObject({ tag: 'android-v1.5.0', version: '1.5.0', notes: 'notes for android-v1.5.0', size: 1234 })
    expect(found?.apkUrl).toContain('openchat-android-v1.5.0.apk')
  })

  it('offers nothing when the installed version is the newest', () => {
    expect(newestAppRelease([release('android-v1.4.0'), release('android-v1.3.0')], '1.4.0')).toBeNull()
  })

  it('leaves out drafts and releases without the APK', () => {
    const list = [release('android-v1.6.0', { draft: true }), release('android-v1.5.0', { assets: [] }), release('android-v1.4.1')]
    expect(newestAppRelease(list, '1.4.0')?.tag).toBe('android-v1.4.1')
  })

  it('follows only https download links', () => {
    const plain = release('android-v1.5.0', { assets: [{ ...apk('v1.5.0'), browser_download_url: 'http://example.com/a.apk' }] })
    expect(newestAppRelease([plain], '1.4.0')).toBeNull()
  })

  it('offers pre-releases only to an installed pre-release', () => {
    const list = [release('android-v1.5.0-rc.2'), release('android-v1.4.1')]
    expect(newestAppRelease(list, '1.4.0')?.tag).toBe('android-v1.4.1')
    expect(newestAppRelease(list, '1.5.0-rc.1')?.tag).toBe('android-v1.5.0-rc.2')
    // The release itself is newer than its candidates.
    expect(newestAppRelease([...list, release('android-v1.5.0')], '1.5.0-rc.1')?.tag).toBe('android-v1.5.0')
  })

  it('offers nothing to a development build that is not a version', () => {
    expect(newestAppRelease([release('android-v1.4.0')], 'dev')).toBeNull()
  })
})
