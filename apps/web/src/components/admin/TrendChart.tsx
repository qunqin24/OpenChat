import type { TrendPoint } from '@openchat/api-client'
import { useMemo } from 'react'

const WIDTH = 720
const HEIGHT = 180
const PADDING = 12

type Series = 'messages' | 'conversations' | 'users'

interface TrendChartProps {
  points: TrendPoint[]
  series: Series
  label: string
}

interface Geometry {
  path: string
  area: string
  last: { x: number; y: number } | null
  peak: number
}

/**
 * Activity over the trend window, drawn by hand.
 *
 * No chart library: a single-series line needs a path and a scale, and pulling
 * in a charting runtime for that would cost more than the component. It also
 * means the line inherits the theme's colours through CSS rather than needing
 * them passed in as props.
 */
function buildGeometry(points: TrendPoint[], series: Series): Geometry {
  if (points.length === 0) return { path: '', area: '', last: null, peak: 0 }

  const values = points.map((point) => Number(point[series] ?? 0))
  // A flat-zero series would divide by zero; floor the scale at 1 so it draws
  // along the baseline instead of vanishing.
  const peak = Math.max(1, ...values)

  const stepX = points.length > 1 ? (WIDTH - PADDING * 2) / (points.length - 1) : 0
  const usableY = HEIGHT - PADDING * 2

  const coords = values.map((value, index) => ({
    x: PADDING + stepX * index,
    y: PADDING + usableY - (value / peak) * usableY,
  }))

  const path = coords
    .map((point, index) => `${index === 0 ? 'M' : 'L'}${point.x.toFixed(2)} ${point.y.toFixed(2)}`)
    .join(' ')

  const first = coords[0]
  const last = coords[coords.length - 1]
  const area =
    first && last
      ? `${path} L${last.x.toFixed(2)} ${HEIGHT - PADDING} L${first.x.toFixed(2)} ${HEIGHT - PADDING} Z`
      : ''

  return { path, area, last: last ?? null, peak }
}

export function TrendChart({ points, series, label }: TrendChartProps) {
  const geometry = useMemo(() => buildGeometry(points, series), [points, series])

  const total = points.reduce((sum, point) => sum + Number(point[series] ?? 0), 0)

  return (
    <div className="n-admin-chart">
      <div className="n-admin-chart-head">
        <span className="n-admin-chart-label">{label}</span>
        <span className="n-admin-chart-total">{total.toLocaleString()}</span>
      </div>

      <svg
        className="n-admin-chart-canvas"
        viewBox={`0 0 ${WIDTH} ${HEIGHT}`}
        preserveAspectRatio="none"
        role="img"
        aria-label={label}
      >
        {[PADDING, HEIGHT / 2, HEIGHT - PADDING].map((y) => (
          <line
            key={y}
            className="n-model-chart-gridline"
            x1={PADDING}
            x2={WIDTH - PADDING}
            y1={y}
            y2={y}
          />
        ))}

        {geometry.area && <path className="n-admin-chart-area" d={geometry.area} />}
        {geometry.path && <path className="n-model-chart-line" d={geometry.path} />}
        {geometry.last && (
          <circle
            className="n-model-chart-dot"
            cx={geometry.last.x}
            cy={geometry.last.y}
            r="3"
          />
        )}
      </svg>
    </div>
  )
}

export default TrendChart
