/**
 * A plugin's mark (docs/design/plugins-plan.md): its Simple Icons glyph, or its
 * letters, on its brand colour. Shared by the plugins page and the admin page.
 */

/** The glyph's colour on a background: dark on a light brand colour, else white. */
export function glyphColor(background: string): string {
  const hex = /^#?([0-9a-f]{6})$/i.exec(background.trim())?.[1]
  if (!hex) return '#fff'
  const [r, g, b] = [0, 2, 4].map((i) => parseInt(hex.slice(i, i + 2), 16) / 255) as [number, number, number]
  const lin = (c: number) => (c <= 0.03928 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4)
  const luminance = 0.2126 * lin(r) + 0.7152 * lin(g) + 0.0722 * lin(b)
  return luminance > 0.55 ? '#111' : '#fff'
}
