#!/usr/bin/env node
/**
 * Copies the brand marks the model catalogue actually uses out of
 * `@lobehub/icons-static-svg` and into `apps/web/public/model-icons/`.
 *
 * Why a script rather than a dependency the app imports:
 *
 *   - The upstream package is 906 files / ~4.4 MB. The catalogue shows a few
 *     dozen brands, so the rest have no business in a build.
 *   - `@lobehub/icons`, the React flavour, peer-depends on `@lobehub/ui` and
 *     `antd`. This repo already spends real effort keeping two design systems
 *     from bleeding into each other (see AGENTS.md); a third is not on offer.
 *   - Serving them ourselves keeps the page from telling a CDN who is looking
 *     at it — the same call the self-hosted fonts make.
 *
 * The list of what to copy is not kept here. It is read out of the three maps
 * in `apps/web/src/components/model-icon.tsx`, which is the file that decides
 * which mark a provider, a vendor or a model family gets — one list, one place
 * to edit.
 *
 * Run: pnpm sync:model-icons
 */
import { createRequire } from 'node:module'
import { existsSync, mkdirSync, readdirSync, readFileSync, rmSync, statSync, writeFileSync } from 'node:fs'
import { dirname, join, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

const require = createRequire(import.meta.url)
const repoRoot = resolve(dirname(fileURLToPath(import.meta.url)), '..')

const sourceDir = join(dirname(require.resolve('@lobehub/icons-static-svg/package.json')), 'icons')
/**
 * Our own marks, for the few shapes upstream does not draw (Z.ai's mark on a
 * disc, as Z.ai shows it). Same naming rules — `-color` keeps its colours, a
 * flat one paints with `currentColor` and is inverted in dark mode — and a
 * file here wins over an upstream one of the same name.
 */
const customDir = join(repoRoot, 'scripts/model-icons')
const componentPath = join(repoRoot, 'apps/web/src/components/model-icon.tsx')
const outDir = join(repoRoot, 'apps/web/public/model-icons')

/**
 * Pulls the values out of one top-level object literal. Deliberately blunt:
 * the maps are flat `key: 'value'` tables at column 0, and anything else in
 * that file should make this fail loudly rather than quietly copy the wrong
 * set.
 */
function iconNamesFrom(source, constName) {
  const declaration = source.indexOf(`const ${constName}`)
  if (declaration === -1) throw new Error(`${constName} not found in model-icon.tsx`)

  const open = source.indexOf('{', declaration)
  const close = source.indexOf('\n}', open)
  if (open === -1 || close === -1) throw new Error(`could not read the ${constName} literal`)

  const names = [...source.slice(open, close).matchAll(/:\s*'([a-z0-9.-]+)'/g)].map(
    (match) => match[1],
  )
  if (names.length === 0) throw new Error(`${constName} maps nothing`)
  return names
}

const component = readFileSync(componentPath, 'utf8')
const wanted = [
  ...new Set([
    ...iconNamesFrom(component, 'PROVIDER_ICONS'),
    ...iconNamesFrom(component, 'BRAND_ICONS'),
    ...iconNamesFrom(component, 'FAMILY_ICONS'),
  ]),
].sort()

/**
 * A handful of the coloured marks paint most of the artwork with the inherited
 * `currentColor` and reserve the brand hue for one path (Kimi's upstream file
 * painted it white instead, so ours in scripts/model-icons paints it with
 * `currentColor` like the rest). Inside an `<img>` `currentColor` has nothing
 * to inherit from and resolves to black, so those marks half-disappear on a
 * dark background — and they cannot be inverted like the flat ones without
 * wrecking the hue.
 *
 * So each is written twice with the ink pinned: `<name>.svg` dark, for the
 * light theme, and `<name>.dark.svg` light, for the dark one. An SVG inside an
 * image cannot see the page's theme class, and the OS's dark mode is not the
 * app's when someone picks a theme by hand; model-icon.tsx shows whichever
 * file the app's theme wants. Its `INK_ICONS` must name exactly these marks,
 * which is checked below.
 */
const INK = { light: '#18181b', dark: '#fafafa' }
const pinInk = (svg, colour) => svg.replace('>', `><style>svg{color:${colour}}</style>`)

const missing = []
const shouldBeColour = []
const flatWithoutCurrentColor = []
const pinned = []
let bytes = 0

mkdirSync(outDir, { recursive: true })

for (const name of wanted) {
  const custom = join(customDir, `${name}.svg`)
  const from = existsSync(custom) ? custom : join(sourceDir, `${name}.svg`)
  if (!existsSync(from)) {
    missing.push(name)
    continue
  }

  // Lobe ships `<brand>-color.svg` next to the flat `<brand>.svg` when there is
  // colour artwork. Taking the flat one anyway is almost always an oversight,
  // so say so instead of silently shipping the duller mark.
  if (from !== custom && !name.endsWith('-color') && existsSync(join(sourceDir, `${name}-color.svg`))) {
    shouldBeColour.push(name)
  }

  let svg = readFileSync(from, 'utf8')
  const inherits = svg.includes('currentColor')

  // Lobe sizes its marks for inline use: `width="1em" height="1em"` on the
  // root. Inside an <img> that becomes a 16px intrinsic size, and WebKit
  // rasterises an SVG image at its intrinsic size before scaling it, so every
  // mark drawn larger than 16px came out soft. Without them the viewBox alone
  // gives the aspect ratio, and the image is drawn at the size it is shown.
  svg = svg.replace(/^<svg\b[^>]*>/, (tag) =>
    tag.replace(/\s(?:width|height)="[^"]*"/g, '').replace(/\sstyle="[^"]*"/, ''),
  )

  if (name.endsWith('-color')) {
    if (inherits) {
      // Inserted right after the opening <svg …> tag, once per theme.
      const dark = pinInk(svg, INK.dark)
      writeFileSync(join(outDir, `${name}.dark.svg`), dark)
      bytes += Buffer.byteLength(dark)
      svg = pinInk(svg, INK.light)
      pinned.push(name)
    }
  } else if (!inherits) {
    // model-icon.tsx inverts flat marks in dark mode on the strength of the
    // name alone. A flat file painted with a fixed colour would be inverted
    // into something else entirely, and nothing else would notice.
    flatWithoutCurrentColor.push(name)
  }

  writeFileSync(join(outDir, `${name}.svg`), svg)
  bytes += Buffer.byteLength(svg)
}

// Keep the directory curated: a mark dropped from the maps should not linger.
for (const file of readdirSync(outDir)) {
  if (!file.endsWith('.svg')) continue
  const name = file.endsWith('.dark.svg') ? file.slice(0, -9) : file.slice(0, -4)
  const kept = file.endsWith('.dark.svg') ? pinned.includes(name) : wanted.includes(name)
  if (!kept) rmSync(join(outDir, file))
}

// The component shows a theme's file only for the marks it lists as ink.
const inkListed = iconNamesFrom(component, 'INK_ICONS').sort()
const inkWritten = [...pinned].sort()
const inkMismatch = inkListed.join() !== inkWritten.join()
if (inkMismatch) {
  console.error(`INK_ICONS in model-icon.tsx should be: ${inkWritten.join(', ')} (it lists ${inkListed.join(', ')})`)
}

if (missing.length > 0) {
  console.error(`Not in @lobehub/icons-static-svg or scripts/model-icons: ${missing.join(', ')}`)
}
if (shouldBeColour.length > 0) {
  console.error(`A -color variant exists upstream for: ${shouldBeColour.join(', ')}`)
}
if (flatWithoutCurrentColor.length > 0) {
  console.error(
    `Flat marks painted with a fixed colour, so dark-mode inversion will be wrong: ${flatWithoutCurrentColor.join(', ')}`,
  )
}
if (pinned.length > 0) {
  console.log(`ink pinned per theme: ${pinned.join(', ')}`)
}

const kib = (bytes / 1024).toFixed(1)
console.log(`${wanted.length - missing.length} icons -> apps/web/public/model-icons (${kib} KiB)`)
console.log(`largest: ${
  readdirSync(outDir)
    .map((file) => [file, statSync(join(outDir, file)).size])
    .sort((a, b) => b[1] - a[1])
    .slice(0, 3)
    .map(([file, size]) => `${file} ${(size / 1024).toFixed(1)} KiB`)
    .join(', ')
}`)

if (missing.length > 0 || shouldBeColour.length > 0 || flatWithoutCurrentColor.length > 0 || inkMismatch) {
  process.exit(1)
}
