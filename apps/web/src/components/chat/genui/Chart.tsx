import { useLayoutEffect, useRef, useState, type PointerEvent } from 'react'
import { useTranslation } from 'react-i18next'

import { cn } from '@/lib/utils'

export interface ChartSeries {
  name: string
  values: number[]
}

interface ChartProps {
  kind: 'line' | 'area' | 'bar' | 'donut' | 'pie'
  labels: string[]
  series: ChartSeries[]
  title?: string
  caption?: string
  unit?: string
}

/** Series colours, in order; defined per theme as --n-chart-1…10. */
const COLORS = 10
const color = (i: number) => `var(--n-chart-${(i % COLORS) + 1})`

const PLOT_HEIGHT = 240
const PAD = { top: 12, right: 12, bottom: 28, left: 44 }
// Bar labels that do not fit side by side are slanted this far, and cut to
// this many characters.
const SLANT_DEGREES = 35
const SLANT_SIN = Math.sin((SLANT_DEGREES * Math.PI) / 180)
const SLANT_COS = Math.cos((SLANT_DEGREES * Math.PI) / 180)
const SLANT_CHARS = 14

/** A label's width at the tick size: a CJK character about 12px, anything else about 7px. */
function textWidth(label: string): number {
  return [...label].reduce((w, c) => w + (c.charCodeAt(0) > 0x2e80 ? 12 : 7), 0)
}

/** The label cut to at most `chars` characters, an ellipsis marking the cut. */
function clip(label: string, chars: number): string {
  const all = [...label]
  return all.length > chars ? `${all.slice(0, chars - 1).join('')}…` : label
}

/**
 * A chart drawn as plain SVG at the width it is given — no charting library:
 * five kinds cover what an answer needs, and drawing them here keeps them in
 * the chat's own type, colours and spacing. Hovering (or tapping) shows the
 * values at that point; a table of the same numbers is there for screen
 * readers.
 */
export function Chart({ kind, labels, series, title, caption, unit }: ChartProps) {
  const { i18n } = useTranslation()
  const frame = useRef<HTMLDivElement>(null)
  const [width, setWidth] = useState(0)
  const [hover, setHover] = useState<number | null>(null)

  useLayoutEffect(() => {
    const el = frame.current
    if (!el) return
    const observer = new ResizeObserver(([entry]) => setWidth(Math.floor(entry?.contentRect.width ?? 0)))
    observer.observe(el)
    return () => observer.disconnect()
  }, [])

  const number = new Intl.NumberFormat(i18n.resolvedLanguage, { maximumFractionDigits: 2 })
  const compact = new Intl.NumberFormat(i18n.resolvedLanguage, { notation: 'compact', maximumFractionDigits: 1 })
  // A symbol or a one-character unit sits on the number (12%, 3元); a word
  // gets a space (2 相对分值, 40 ms), or it reads as part of the number.
  const spaced = unit && [...unit].length > 1 && /^[\p{L}]/u.test(unit) ? ` ${unit}` : (unit ?? '')
  const format = (v: number) => (Number.isFinite(v) ? `${number.format(v)}${spaced}` : '—')

  const round = kind === 'donut' || kind === 'pie'
  const shown = round ? series.slice(0, 1) : series

  return (
    <figure className="n-ui-chart">
      {title && <figcaption className="n-ui-chart-title">{title}</figcaption>}
      <div ref={frame} className="n-ui-chart-frame">
        {width > 0 &&
          (round ? (
            <Round kind={kind} labels={labels} values={shown[0]?.values ?? []} hover={hover} onHover={setHover} format={format} />
          ) : (
            <Cartesian
              kind={kind}
              width={width}
              labels={labels}
              series={shown}
              hover={hover}
              onHover={setHover}
              tick={(v) => compact.format(v)}
              format={format}
            />
          ))}
      </div>
      {(round || shown.length > 1) && (
        <ul className="n-ui-chart-legend">
          {(round ? labels : shown.map((s) => s.name)).map((name, i) => (
            <li key={i} className={cn(round && hover !== null && hover !== i && 'is-dim')}>
              <span className="n-ui-chart-swatch" style={{ background: color(i) }} />
              {name}
            </li>
          ))}
        </ul>
      )}
      {caption && <p className="n-ui-chart-caption">{caption}</p>}
      <table className="sr-only">
        {title && <caption>{title}</caption>}
        <thead>
          <tr>
            <th scope="col" />
            {shown.map((s, i) => (
              <th key={i} scope="col">{s.name}</th>
            ))}
          </tr>
        </thead>
        <tbody>
          {labels.map((label, r) => (
            <tr key={r}>
              <th scope="row">{label}</th>
              {shown.map((s, i) => (
                <td key={i}>{format(s.values[r] ?? NaN)}</td>
              ))}
            </tr>
          ))}
        </tbody>
      </table>
    </figure>
  )
}

interface CartesianProps {
  kind: 'line' | 'area' | 'bar'
  width: number
  labels: string[]
  series: ChartSeries[]
  hover: number | null
  onHover: (index: number | null) => void
  tick: (value: number) => string
  format: (value: number) => string
}

function Cartesian({ kind, width, labels, series, hover, onHover, tick, format }: CartesianProps) {
  const n = labels.length
  const values = series.flatMap((s) => s.values.slice(0, n)).filter(Number.isFinite)
  const bars = kind === 'bar'
  // Bars and areas stand on zero; lines may float, so their scale hugs the data.
  const [min, max, step] = niceScale(
    bars || kind === 'area' ? Math.min(0, ...values) : Math.min(...values),
    Math.max(...values, bars ? 0 : -Infinity),
  )
  // Labels slanted under bars reach left of their bar: the first one is
  // given room by widening the left margin.
  const fit = (left: number) => Math.max(width - left - PAD.right, 1) / Math.max(n, 1)
  const labelWidth = Math.max(...labels.map(textWidth), 0) + 14
  const slant = bars && labelWidth > fit(PAD.left)
  const shownLabels = slant ? labels.map((l) => clip(l, SLANT_CHARS)) : labels
  const firstReach = slant ? textWidth(shownLabels[0] ?? '') * SLANT_COS : 0
  const left = Math.max(PAD.left, Math.ceil(firstReach - fit(PAD.left) / 2 + 4))
  const plotW = Math.max(width - left - PAD.right, 1)
  const band = plotW / Math.max(n, 1)

  // Skip x labels that would collide, judging by the longest. A bar is a
  // category, though, and a bar without its name cannot be read: bars keep
  // every label, slanted when they do not fit side by side, a long one cut
  // short (the tooltip has it whole).
  const every = bars ? 1 : Math.max(1, Math.ceil(n / Math.max(1, Math.floor(plotW / labelWidth))))
  const slantDepth = slant ? Math.max(...shownLabels.map(textWidth), 0) * SLANT_SIN + 12 : 0
  const height = PLOT_HEIGHT + PAD.top + (slant ? Math.ceil(slantDepth) + 14 : PAD.bottom)
  // Lines put a point at each label's centre for bars, at the edges otherwise.
  const x = (i: number) => (bars ? left + band * (i + 0.5) : left + (n > 1 ? (plotW * i) / (n - 1) : plotW / 2))
  const y = (v: number) => PAD.top + PLOT_HEIGHT - ((v - min) / (max - min || 1)) * PLOT_HEIGHT
  const ticks: number[] = []
  for (let v = min; v <= max + step / 2; v += step) ticks.push(Number(v.toPrecision(12)))

  // Where the pointer is over the plot, for the tooltip to follow it.
  const [pointer, setPointer] = useState<{ x: number; y: number } | null>(null)
  const pick = (event: PointerEvent<SVGRectElement>) => {
    const box = event.currentTarget.getBoundingClientRect()
    const px = event.clientX - box.left
    const i = bars ? Math.floor(px / band) : Math.round(n > 1 ? (px / plotW) * (n - 1) : 0)
    onHover(Math.min(n - 1, Math.max(0, i)))
    setPointer({ x: left + px, y: PAD.top + event.clientY - box.top })
  }
  // Beside the pointer, below and to the right; on the other side of it where
  // that would run off the plot's right edge or bottom.
  const tipHeight = 34 + 18 * series.length
  const tipStyle = pointer && {
    left: pointer.x,
    top: pointer.y,
    transform: `translate(${pointer.x > width - 180 ? 'calc(-100% - 14px)' : '14px'}, ${
      pointer.y + 14 + tipHeight > height ? 'calc(-100% - 14px)' : '14px'
    })`,
  }

  const groupW = band * 0.64
  const barW = groupW / Math.max(series.length, 1)

  return (
    <div className="n-ui-chart-plot">
      <svg width={width} height={height} aria-hidden="true">
        {ticks.map((t) => (
          <g key={t}>
            <line className="n-ui-chart-grid" x1={left} x2={width - PAD.right} y1={y(t)} y2={y(t)} />
            <text className="n-ui-chart-tick" x={left - 10} y={y(t)} textAnchor="end" dominantBaseline="middle">
              {tick(t)}
            </text>
          </g>
        ))}
        {shownLabels.map((label, i) =>
          slant ? (
            <text
              key={i}
              className="n-ui-chart-tick"
              x={x(i)}
              y={PAD.top + PLOT_HEIGHT + 14}
              textAnchor="end"
              transform={`rotate(${-SLANT_DEGREES} ${x(i)} ${PAD.top + PLOT_HEIGHT + 14})`}
            >
              {label}
            </text>
          ) : i % every === 0 ? (
            <text key={i} className="n-ui-chart-tick" x={x(i)} y={height - 8} textAnchor={bars || (i > 0 && i < n - 1) ? 'middle' : i === 0 ? 'start' : 'end'}>
              {label}
            </text>
          ) : null,
        )}
        {hover !== null && !bars && <line className="n-ui-chart-guide" x1={x(hover)} x2={x(hover)} y1={PAD.top} y2={PAD.top + PLOT_HEIGHT} />}

        {bars
          ? series.map((s, si) =>
              s.values.slice(0, n).map((v, i) => {
                if (!Number.isFinite(v)) return null
                const top = y(Math.max(v, 0))
                const h = Math.max(Math.abs(y(v) - y(0)), 1)
                const bx = left + band * i + (band - groupW) / 2 + barW * si
                return (
                  <path
                    key={`${si}-${i}`}
                    className={cn('n-ui-chart-bar', hover !== null && hover !== i && 'is-dim')}
                    d={roundedTop(bx + 1, top, Math.max(barW - 2, 1), h, Math.min(4, barW / 2))}
                    fill={color(si)}
                  />
                )
              }),
            )
          : series.map((s, si) => {
              const path = linePath(s.values.slice(0, n), x, y)
              return (
                <g key={si}>
                  {kind === 'area' && (
                    <path className="n-ui-chart-area" d={areaPath(s.values.slice(0, n), x, y, y(Math.max(min, 0)))} fill={color(si)} />
                  )}
                  <path className="n-ui-chart-line" d={path} stroke={color(si)} />
                  {n <= 24 &&
                    s.values.slice(0, n).map((v, i) =>
                      Number.isFinite(v) ? (
                        <circle key={i} className="n-ui-chart-dot" cx={x(i)} cy={y(v)} r={hover === i ? 5 : 3.5} fill={color(si)} />
                      ) : null,
                    )}
                </g>
              )
            })}
        <rect
          className="n-ui-chart-hit"
          x={left}
          y={PAD.top}
          width={plotW}
          height={PLOT_HEIGHT}
          onPointerMove={pick}
          onPointerDown={pick}
          onPointerLeave={() => {
            onHover(null)
            setPointer(null)
          }}
        />
      </svg>
      {hover !== null && tipStyle && (
        <div className="n-ui-chart-tip" style={tipStyle} role="presentation">
          <div className="n-ui-chart-tip-label">{labels[hover]}</div>
          {series.map((s, si) => (
            <div key={si} className="n-ui-chart-tip-row">
              <span className="n-ui-chart-swatch" style={{ background: color(si) }} />
              {series.length > 1 && <span className="n-ui-chart-tip-name">{s.name}</span>}
              <span className="n-ui-chart-tip-value">{format(s.values[hover] ?? NaN)}</span>
            </div>
          ))}
        </div>
      )}
    </div>
  )
}

interface RoundProps {
  kind: 'donut' | 'pie'
  labels: string[]
  values: number[]
  hover: number | null
  onHover: (index: number | null) => void
  format: (value: number) => string
}

function Round({ kind, labels, values, hover, onHover, format }: RoundProps) {
  const size = 220
  const r = size / 2
  const inner = kind === 'donut' ? r * 0.56 : 0
  const slices = labels.map((_, i) => Math.max(values[i] ?? 0, 0)).map((v) => (Number.isFinite(v) ? v : 0))
  const total = slices.reduce((a, b) => a + b, 0) || 1
  // Starts at twelve o'clock and runs clockwise, as a reader expects.
  const start = -Math.PI / 2
  const arcs = slices.map((v, i) => {
    const before = slices.slice(0, i).reduce((a, b) => a + b, 0)
    const from = start + (before / total) * Math.PI * 2
    return { i, from, to: from + (v / total) * Math.PI * 2, value: v }
  })
  const focus = hover !== null ? arcs[hover] : null

  return (
    <div className="n-ui-chart-round" onPointerLeave={() => onHover(null)}>
      <svg width={size} height={size} viewBox={`0 0 ${size} ${size}`} aria-hidden="true">
        {arcs.map((a) =>
          a.value > 0 ? (
            <path
              key={a.i}
              className={cn('n-ui-chart-slice', hover !== null && hover !== a.i && 'is-dim')}
              d={slicePath(r, r, hover === a.i ? r : r - 4, inner, a.from, a.to)}
              fill={color(a.i)}
              onPointerEnter={() => onHover(a.i)}
              onPointerDown={() => onHover(a.i)}
            />
          ) : null,
        )}
      </svg>
      {kind === 'donut' && focus && (
        <div className="n-ui-chart-center">
          <div className="n-ui-chart-center-value">{Math.round((focus.value / total) * 100)}%</div>
          <div className="n-ui-chart-center-label">{labels[focus.i]}</div>
          <div className="n-ui-chart-center-sub">{format(focus.value)}</div>
        </div>
      )}
    </div>
  )
}

/** Round bounds and a step that make tidy ticks: 0, 90, 180… not 0, 87, 174. */
function niceScale(lo: number, hi: number): [number, number, number] {
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
function linePath(values: number[], x: (i: number) => number, y: (v: number) => number) {
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

function areaPath(values: number[], x: (i: number) => number, y: (v: number) => number, base: number) {
  const points = values.map((v, i) => [i, v] as const).filter(([, v]) => Number.isFinite(v))
  if (points.length === 0) return ''
  const top = points.map(([i, v]) => `${x(i).toFixed(1)},${y(v).toFixed(1)}`).join('L')
  const first = x(points[0]![0]).toFixed(1)
  const last = x(points[points.length - 1]![0]).toFixed(1)
  return `M${first},${base}L${top}L${last},${base}Z`
}

function roundedTop(x: number, y: number, w: number, h: number, r: number) {
  const rr = Math.min(r, h)
  return `M${x},${y + h}V${y + rr}Q${x},${y} ${x + rr},${y}H${x + w - rr}Q${x + w},${y} ${x + w},${y + rr}V${y + h}Z`
}

function slicePath(cx: number, cy: number, outer: number, inner: number, from: number, to: number): string {
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

export default Chart
