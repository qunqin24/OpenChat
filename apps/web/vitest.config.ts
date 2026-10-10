import { defineConfig, mergeConfig } from 'vitest/config'

import viteConfig from './vite.config.ts'

// The app's own vite config (aliases, resolution), without its plugins: the
// route generator, Tailwind and the precompressor are for building the app,
// and the tests here are of plain modules that need none of them.
export default mergeConfig(
  { ...viteConfig, plugins: [] },
  defineConfig({
    test: {
      include: ['src/**/*.test.ts'],
      environment: 'node',
    },
  }),
)
