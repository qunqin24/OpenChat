/**
 * Copies the generative UI component catalogue to the two places that read it:
 * the server, which embeds it to build the system prompt, and the API client,
 * which the web app validates and renders against.
 *
 * One file, two readers, so the components the model is told about and the
 * components the page can draw cannot drift apart.
 *
 * Run: make generate-genui
 */
import { readFileSync, writeFileSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'

const root = join(dirname(fileURLToPath(import.meta.url)), '..')
const source = JSON.parse(readFileSync(join(root, 'api/genui-catalog.json'), 'utf8'))

const generated = {
  $comment: 'GENERATED from api/genui-catalog.json by `make generate-genui`. DO NOT EDIT.',
  ...Object.fromEntries(Object.entries(source).filter(([key]) => key !== '$comment')),
}
const text = `${JSON.stringify(generated, null, 2)}\n`

for (const target of [
  'apps/server/internal/genui/catalog.gen.json',
  'packages/api-client/src/genui-catalog.gen.json',
]) {
  writeFileSync(join(root, target), text)
  console.log(`genui catalogue -> ${target}`)
}
