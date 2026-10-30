// The geometry of an answer's chart (GenUI Chart): sizes, the scale and its
// ticks, and the SVG paths of lines, areas, bars and slices. Pure, so the web's
// Chart.tsx and the app's draw the same chart.

export const PLOT_HEIGHT = 240
export const PAD = { top: 12, right: 12, bottom: 28, left: 44 }
// Bar labels that do not fit side by side are slanted this far, and cut to
// this many characters.
export const SLANT_DEGREES = 35
export const SLANT_SIN = Math.sin((SLANT_DEGREES * Math.PI) / 180)
export const SLANT_COS = Math.cos((SLANT_DEGREES * Math.PI) / 180)
export const SLANT_CHARS = 14

/** A label's width at the tick size: a CJK character about 12px, anything else about 7px. */
export function textWidth(label: string): number {
  return [...label].reduce((w, c) => w + (c.charCodeAt(0) > 0x2e80 ? 12 : 7), 0)
}

/** The label cut to at most `chars` characters, an ellipsis marking the cut. */
export function clip(label: string, chars: number): string {
  const all = [...label]
  return all.length > chars ? `${all.slice(0, chars - 1).join('')}…` : label
}

/** Round bounds and a step that make tidy ticks: 0, 90, 180… not 0, 87, 174. */
export function niceScale(lo: number, hi: number): [number, number, number] {
  if (!Number.isFinite(lo) || !Number.isFinite(hi)) return [0, 1, 0.25]
  if (lo === hi) {
    const pad = Math.abs(lo) || 1
    lo -= pad
    hi += pad
  }
  const rough = (hi - lo) / 4
  const magnitude = 10 ** Math.floor(Math.log10(rough))
  const step = ([1, 2, 2.5, 5, 10].find((m) => m * magnitude >= rough) ?? 10) * magnitude
  return [Math.floor(lo / step) * step, Math.ceil(hi / step) * step, step]
}

/** A line through the finite points; a missing value breaks it. */
export function linePath(values: number[], x: (i: number) => number, y: (v: number) => number) {
  let d = ''
  let pen = false
  values.forEach((v, i) => {
    if (!Number.isFinite(v)) {
      pen = false
      return
    }
    d += `${pen ? 'L' : 'M'}${x(i).toFixed(1)},${y(v).toFixed(1)}`
    pen = true
  })
  return d
}

export function areaPath(values: number[], x: (i: number) => number, y: (v: number) => number, base: number) {
  const points = values.map((v, i) => [i, v] as const).filter(([, v]) => Number.isFinite(v))
  if (points.length === 0) return ''
  const top = points.map(([i, v]) => `${x(i).toFixed(1)},${y(v).toFixed(1)}`).join('L')
  const first = x(points[0]![0]).toFixed(1)
  const last = x(points[points.length - 1]![0]).toFixed(1)
  return `M${first},${base}L${top}L${last},${base}Z`
}

export function roundedTop(x: number, y: number, w: number, h: number, r: number) {
  const rr = Math.min(r, h)
  return `M${x},${y + h}V${y + rr}Q${x},${y} ${x + rr},${y}H${x + w - rr}Q${x + w},${y} ${x + w},${y + rr}V${y + h}Z`
}

export function slicePath(cx: number, cy: number, outer: number, inner: number, from: number, to: number): string {
  // A whole circle cannot be one arc; draw it as two halves.
  if (to - from >= Math.PI * 2 - 1e-6) {
    const mid = from + Math.PI
    return slicePath(cx, cy, outer, inner, from, mid) + slicePath(cx, cy, outer, inner, mid, to)
  }
  const large = to - from > Math.PI ? 1 : 0
  const p = (radius: number, a: number) => `${(cx + radius * Math.cos(a)).toFixed(2)},${(cy + radius * Math.sin(a)).toFixed(2)}`
  if (inner <= 0) return `M${cx},${cy}L${p(outer, from)}A${outer},${outer} 0 ${large} 1 ${p(outer, to)}Z`
  return `M${p(outer, from)}A${outer},${outer} 0 ${large} 1 ${p(outer, to)}L${p(inner, to)}A${inner},${inner} 0 ${large} 0 ${p(inner, from)}Z`
}
