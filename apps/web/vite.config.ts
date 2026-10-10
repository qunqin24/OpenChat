import { readdir, readFile, stat, writeFile } from 'node:fs/promises'
import { extname, join, resolve } from 'node:path'
import { fileURLToPath, URL } from 'node:url'
import { promisify } from 'node:util'
import { brotliCompress, constants as zlib, gzip } from 'node:zlib'

import tailwindcss from '@tailwindcss/vite'
import { tanstackRouter } from '@tanstack/router-plugin/vite'
import react from '@vitejs/plugin-react'
import { defineConfig, type Plugin } from 'vite'

const brotli = promisify(brotliCompress)
const gzipAsync = promisify(gzip)

// Text formats worth compressing. woff and woff2 are already compressed
// internally, and images are left to their own codecs.
const COMPRESSIBLE = new Set([
  '.js', '.mjs', '.css', '.html', '.svg', '.json', '.txt', '.xml', '.map',
  '.webmanifest', '.ttf', '.otf',
])
// Below this a response fits in one packet either way.
const MIN_BYTES = 256

async function* walk(dir: string): AsyncGenerator<string> {
  for (const entry of await readdir(dir, { withFileTypes: true })) {
    const full = join(dir, entry.name)
    if (entry.isDirectory()) yield* walk(full)
    else if (entry.isFile()) yield full
  }
}

/**
 * Writes `<file>.br` and `<file>.gz` next to every compressible build output
 * that gets smaller for it, so the Go server (internal/webui) can send them
 * as-is instead of compressing per request. Brotli at maximum quality is far
 * too slow to do on the request path, which is why it happens here once.
 *
 * Uses node's built-in zlib only. The sidecars live in dist/ beside their
 * originals; the server picks one by Accept-Encoding.
 */
function precompress(): Plugin {
  let outDir = ''
  return {
    name: 'openchat:precompress',
    apply: 'build',
    configResolved(config) {
      outDir = resolve(config.root, config.build.outDir)
    },
    closeBundle: {
      // After every other plugin has finished writing into outDir.
      order: 'post',
      sequential: true,
      async handler() {
        const jobs: Promise<void>[] = []
        for await (const file of walk(outDir)) {
          if (!COMPRESSIBLE.has(extname(file).toLowerCase())) continue
          jobs.push(
            (async () => {
              const { size } = await stat(file)
              if (size < MIN_BYTES) return
              const source = await readFile(file)
              const [br, gz] = await Promise.all([
                brotli(source, {
                  params: {
                    [zlib.BROTLI_PARAM_MODE]: zlib.BROTLI_MODE_TEXT,
                    [zlib.BROTLI_PARAM_QUALITY]: zlib.BROTLI_MAX_QUALITY,
                    [zlib.BROTLI_PARAM_LGWIN]: zlib.BROTLI_MAX_WINDOW_BITS,
                    [zlib.BROTLI_PARAM_SIZE_HINT]: source.length,
                  },
                }),
                gzipAsync(source, { level: 9 }),
              ])
              if (br.length < source.length) await writeFile(`${file}.br`, br)
              if (gz.length < source.length) await writeFile(`${file}.gz`, gz)
            })(),
          )
        }
        await Promise.all(jobs)
      },
    },
  }
}

export default defineConfig({
  plugins: [
    // Must precede the React plugin: it generates routeTree.gen.ts from src/routes.
    tanstackRouter({ target: 'react', autoCodeSplitting: true }),
    react(),
    tailwindcss(),
    precompress(),
  ],
  resolve: {
    alias: {
      '@': fileURLToPath(new URL('./src', import.meta.url)),
    },
  },
  server: {
    port: 5173,
    proxy: {
      // Keeps the browser same-origin in dev, so SSE and cookies just work.
      '/api': {
        target: process.env.VITE_PROXY_TARGET ?? 'http://localhost:8080',
        changeOrigin: true,
      },
    },
  },
  build: {
    sourcemap: true,
  },
})
