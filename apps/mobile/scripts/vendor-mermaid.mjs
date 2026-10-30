// Copies Mermaid's browser build into the app as a module that exports its
// source as a string (src/vendor/mermaid.gen.js, not checked in): the diagram
// preview runs it inside a WebView, which has no other way to reach it without
// fetching it from a third party. Run on install (package.json "postinstall").
import { createRequire } from 'node:module'
import { mkdirSync, readFileSync, writeFileSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'

const require = createRequire(import.meta.url)
const source = readFileSync(require.resolve('mermaid/dist/mermaid.min.js'), 'utf8')
// The preview puts it inline in a <script>: a version holding a sequence that
// would end that element early must not slip through unnoticed.
if (/<\/?script/i.test(source)) throw new Error('mermaid.min.js contains "<script" or "</script"; it cannot be inlined')
const { version } = JSON.parse(readFileSync(require.resolve('mermaid/package.json'), 'utf8'))
const out = join(dirname(fileURLToPath(import.meta.url)), '..', 'src', 'vendor', 'mermaid.gen.js')
mkdirSync(dirname(out), { recursive: true })
writeFileSync(out, `// GENERATED from mermaid@${version} by scripts/vendor-mermaid.mjs. DO NOT EDIT.\nmodule.exports = ${JSON.stringify(source)}\n`)
console.log(`vendored mermaid@${version} (${Math.round(source.length / 1024)} KB)`)
