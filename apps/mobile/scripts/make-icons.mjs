// Draws the app's icon, adaptive icon layers and splash image from the brand
// SVGs, so they stay one mark with the website's: design/logo-tangent.svg (the
// thirty-circle tile) and apps/web/public/logo-mark.svg (the six-circle mark).
// The results are checked in under assets/; run
// `node scripts/make-icons.mjs` from apps/mobile after the mark changes.
import { mkdirSync, readFileSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'
import sharp from 'sharp'

const root = join(dirname(fileURLToPath(import.meta.url)), '..', '..', '..')
const out = join(root, 'apps', 'mobile', 'assets')
mkdirSync(out, { recursive: true })

const tangent = readFileSync(join(root, 'design', 'logo-tangent.svg'), 'utf8')
const mark = readFileSync(join(root, 'apps', 'web', 'public', 'logo-mark.svg'), 'utf8')
const favicon = readFileSync(join(root, 'apps', 'web', 'public', 'favicon.svg'), 'utf8')

const DARK = '#0A0B0D'
// The circles of the tangent logo, without its rounded tile.
const circles = /<g stroke-width[\s\S]*<\/g>/.exec(tangent)?.[0]
// The mark's circles; it is drawn in black and only its opacity matters.
const markCircles = /<g stroke[\s\S]*<\/g>/.exec(mark)?.[0]
if (!circles || !markCircles) throw new Error('the brand SVGs changed shape; update scripts/make-icons.mjs')

/** An SVG of `size` px holding `body`, whose 512-unit design is scaled by `scale` about the middle. */
const canvas = (size, body, background = '') =>
  `<svg xmlns="http://www.w3.org/2000/svg" width="${size}" height="${size}" viewBox="0 0 512 512" fill="none">${background}${body}</svg>`

// The circles are centred on x=252, not 256: they all touch at x=402. Shift
// them so the middle of the drawing sits in the middle of the canvas.
const centred = (scale) => `<g transform="translate(256 256) scale(${scale}) translate(-252 -256)">${circles}</g>`

const png = (svg, file) => sharp(Buffer.from(svg)).png().toFile(join(out, file))

await Promise.all([
  // The launcher icon on devices without adaptive icons: a full square, the
  // system rounds it.
  png(canvas(1024, centred(1.15), `<rect width="512" height="512" fill="${DARK}"/>`), 'icon.png'),

  // The adaptive icon: Android shows only the middle 66% of each 108dp layer
  // whatever mask the launcher picks, so the circles (300 of 512 units across)
  // stay within it, and the dark ground is its own layer.
  png(canvas(1024, centred(1.0)), 'android-icon-foreground.png'),
  png(canvas(1024, '', `<rect width="512" height="512" fill="${DARK}"/>`), 'android-icon-background.png'),

  // Android 13's themed icons tint the layer by its alpha, so the six-circle
  // mark (heavier strokes than the thirty, which would blur into a disc at
  // launcher sizes) in one colour. It spans 30 of 32 units; scaled by 9.6 it is
  // 288 of 512 units across, as wide as the foreground.
  png(
    canvas(1024, `<g transform="translate(256 256) scale(9.6) translate(-16 -16)">${markCircles}</g>`),
    'android-icon-monochrome.png',
  ),

  // The splash screen shows this over white or #161616 depending on the
  // theme, so it carries its own dark tile (as the favicon does) and reads on
  // both.
  sharp(Buffer.from(tangent)).resize(1024, 1024).png().toFile(join(out, 'splash-icon.png')),

  sharp(Buffer.from(favicon)).resize(48, 48).png().toFile(join(out, 'favicon.png')),
])
console.log('wrote icons to', out)
