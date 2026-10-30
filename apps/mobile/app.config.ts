import type { ConfigContext, ExpoConfig } from 'expo/config'

/**
 * The app's build-time settings. A fork rebuilds with its own server, package
 * and releases repository by setting the environment, not by editing this:
 *
 * - APP_VERSION: the app's release, from its git tag without "android-v"
 *   (android-v1.4.0 → 1.4.0, android-v1.4.0-rc.1 → 1.4.0-rc.1); the Android
 *   versionCode follows from it and always grows.
 * - APP_VARIANT=development: a separate package (".dev") that may use plain
 *   http, for a development server on the emulator's host (10.0.2.2).
 * - APP_ID: the Android package (default org.qunqin.openchat).
 * - EXPO_PUBLIC_SERVER_URL / EXPO_PUBLIC_RELEASES_REPO: the server the app
 *   talks to, and the GitHub repository its updates come from.
 */
const version = process.env.APP_VERSION ?? '0.0.0'
const development = process.env.APP_VARIANT === 'development'
const appId = (process.env.APP_ID ?? 'org.qunqin.openchat') + (development ? '.dev' : '')

/** 1.4.0-rc.2 → 1040002, 1.4.0 → 1040099: a release outranks its candidates. */
export function versionCode(v: string): number {
  const match = /^(\d+)\.(\d+)\.(\d+)(?:-rc\.(\d+))?$/.exec(v)
  if (!match) return 1
  const [, major, minor, patch, rc] = match.map(Number) as number[]
  return major! * 1_000_000 + minor! * 10_000 + patch! * 100 + (rc ? Math.min(rc, 98) : 99)
}

export default ({ config }: ConfigContext): ExpoConfig => ({
  ...config,
  name: development ? 'OpenChat Dev' : 'OpenChat',
  slug: 'openchat',
  scheme: 'openchat',
  version,
  orientation: 'portrait',
  userInterfaceStyle: 'automatic',
  icon: './assets/icon.png',
  android: {
    package: appId,
    versionCode: versionCode(version),
    adaptiveIcon: {
      backgroundColor: '#0A0B0D',
      foregroundImage: './assets/android-icon-foreground.png',
      backgroundImage: './assets/android-icon-background.png',
      monochromeImage: './assets/android-icon-monochrome.png',
    },
    // Handing a downloaded update to the system installer (see src/lib/update.ts).
    permissions: ['android.permission.REQUEST_INSTALL_PACKAGES'],
    predictiveBackGestureEnabled: false,
  },
  ios: { bundleIdentifier: appId, supportsTablet: false },
  plugins: [
    // Signs the release APK with our own key when the release workflow gives it.
    './plugins/with-release-signing',
    'expo-router',
    'expo-status-bar',
    'expo-secure-store',
    'expo-localization',
    // Photos come through the system's picker (no media permission); the
    // camera asks for its own.
    ['expo-image-picker', { photosPermission: false, cameraPermission: 'OpenChat uses the camera to take a photo for your message.', microphonePermission: false }],
    // Sharing text, a picture or a file from another app into a new chat. The
    // iOS share extension comes with the iOS app.
    [
      'expo-share-intent',
      { androidIntentFilters: ['text/*', 'image/*', '*/*'], androidMultiIntentFilters: ['image/*', '*/*'], disableIOS: true },
    ],
    ['expo-splash-screen', { backgroundColor: '#ffffff', dark: { backgroundColor: '#161616' }, image: './assets/splash-icon.png', imageWidth: 96 }],
    [
      'expo-build-properties',
      {
        // Credential Manager's passkeys need Android 9.
        android: { minSdkVersion: 28, usesCleartextTraffic: development },
      },
    ],
  ],
  extra: {
    serverUrl: process.env.EXPO_PUBLIC_SERVER_URL ?? 'https://openchat.qunqin.org',
    releasesRepo: process.env.EXPO_PUBLIC_RELEASES_REPO ?? 'qunqin24/OpenChat',
  },
  experiments: { typedRoutes: true },
})
