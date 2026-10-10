// Copies the web build into the Go server's webui package so `go:embed` can
// reach it (go:embed cannot read outside its module).
//
//   node scripts/embed-web.mjs [from] [to]
//
// Defaults: apps/web/dist -> apps/server/internal/webui/dist. Run after
// `vite build`; `make build` does both.
//
// Source maps stay in apps/web/dist but are left out of the copy, so they are
// not shipped inside the binary. The committed .gitkeep is preserved: it is
// what lets `go build` work before any web build exists.
import { cpSync, existsSync, mkdirSync, readdirSync, rmSync } from 'node:fs'
import { dirname, join, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

const root = resolve(dirname(fileURLToPath(import.meta.url)), '..')
const from = resolve(process.argv[2] ?? join(root, 'apps/web/dist'))
const to = resolve(process.argv[3] ?? join(root, 'apps/server/internal/webui/dist'))

if (!existsSync(join(from, 'index.html'))) {
  console.error(`embed-web: ${from}/index.html not found; run the web build first`)
  process.exit(1)
}

mkdirSync(to, { recursive: true })
for (const entry of readdirSync(to)) {
  if (entry !== '.gitkeep') rmSync(join(to, entry), { recursive: true, force: true })
}

const isSourceMap = (name) => /\.map(\.br|\.gz)?$/.test(name)
cpSync(from, to, { recursive: true, filter: (src) => !isSourceMap(src) })
console.log(`embed-web: ${from} -> ${to}`)
