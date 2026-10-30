/* global module, require, process */
// Signs the release build with our own keystore instead of the debug key the
// generated project uses by default.
//
// Android only installs an update over an app when both are signed with the
// same key, so every release APK must carry the one release key. The in-app
// updater downloads a release and hands it to the system installer, which
// refuses it if the signature differs, and the user would then have to
// uninstall the app and lose its local state. The key stays out of the
// repository; the release workflow decodes it from a secret and names it in:
//
//   ANDROID_KEYSTORE_PATH       the .jks file
//   ANDROID_KEYSTORE_PASSWORD   its password
//   ANDROID_KEY_ALIAS           the key's alias
//   ANDROID_KEY_PASSWORD        the key's password
//
// Without ANDROID_KEYSTORE_PATH this does nothing and the build keeps the debug
// signing, so a local `expo run:android` or assembleRelease still works (such a
// build can not be updated by a real release, which is signed differently).
// The generated build.gradle reads the values from the environment itself, when
// Gradle runs, so the passwords are not written into the android/ folder.
// Expo loads config plugins with require(), so this file is CommonJS.
// eslint-disable-next-line @typescript-eslint/no-require-imports
const { withAppBuildGradle } = require('expo/config-plugins')

const NAMES = ['ANDROID_KEYSTORE_PATH', 'ANDROID_KEYSTORE_PASSWORD', 'ANDROID_KEY_ALIAS', 'ANDROID_KEY_PASSWORD']

const RELEASE_CONFIG = `
        release {
            storeFile file(System.getenv('ANDROID_KEYSTORE_PATH'))
            storePassword System.getenv('ANDROID_KEYSTORE_PASSWORD')
            keyAlias System.getenv('ANDROID_KEY_ALIAS')
            keyPassword System.getenv('ANDROID_KEY_PASSWORD')
        }`

/** The build.gradle of the generated project with the release signing in. */
function sign(gradle) {
  if (gradle.includes("System.getenv('ANDROID_KEYSTORE_PATH')")) return gradle // already done

  // A release signed with the debug key would look like a success and then
  // never update anyone, so a template that is no longer what this expects
  // stops the build rather than passes.
  const config = /(signingConfigs\s*\{\s*debug\s*\{[^}]*\})/.exec(gradle)
  const type = /(buildTypes\s*\{[\s\S]*?release\s*\{[\s\S]*?)signingConfig signingConfigs\.debug/.exec(gradle)
  if (!config || !type) {
    throw new Error('with-release-signing: the generated android/app/build.gradle has changed; update plugins/with-release-signing.js')
  }
  return gradle
    .replace(config[1], config[1] + RELEASE_CONFIG)
    .replace(type[1] + 'signingConfig signingConfigs.debug', type[1] + 'signingConfig signingConfigs.release')
}

module.exports = function withReleaseSigning(config) {
  if (!process.env.ANDROID_KEYSTORE_PATH) return config
  const missing = NAMES.filter((name) => !process.env[name])
  if (missing.length > 0) {
    throw new Error(`with-release-signing: ANDROID_KEYSTORE_PATH is set but ${missing.join(', ')} ${missing.length > 1 ? 'are' : 'is'} not`)
  }
  return withAppBuildGradle(config, (c) => {
    c.modResults.contents = sign(c.modResults.contents)
    return c
  })
}

module.exports.sign = sign
