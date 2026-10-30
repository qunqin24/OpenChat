import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { StyleSheet, Text, View } from 'react-native'
import Svg, { Circle, G, Line, Path, Rect, Text as SvgText } from 'react-native-svg'

import {
  areaPath,
  clip,
  linePath,
  niceScale,
  PAD,
  PLOT_HEIGHT,
  roundedTop,
  SLANT_CHARS,
  SLANT_COS,
  SLANT_DEGREES,
  SLANT_SIN,
  slicePath,
  textWidth,
} from '@openchat/chat-core/genui/chart-geometry'

import { useColors, type Colors } from '@/theme'

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

/**
 * An answer's chart (the web's genui/Chart.tsx, on react-native-svg; the
 * geometry is shared, chat-core/genui/chart-geometry.ts). A tap picks the
 * point, bar or slice, and its values show under the plot.
 */
export function Chart({ kind, labels, series, title, caption, unit }: ChartProps) {
  const { i18n } = useTranslation()
  const colors = useColors()
  const [width, setWidth] = useState(0)
  const [picked, setPicked] = useState<number | null>(null)

  const number = new Intl.NumberFormat(i18n.language, { maximumFractionDigits: 2 })
  const compact = compactFormat(i18n.language)
  // A symbol or a one-character unit sits on the number (12%, 3元); a word
  // gets a space (2 相对分值, 40 ms).
  const spaced = unit && [...unit].length > 1 && /^[A-Za-z\u00C0-\uFFFF]/.test(unit) ? ` ${unit}` : (unit ?? '')
  const format = (v: number) => (Number.isFinite(v) ? `${number.format(v)}${spaced}` : '—')
  const color = (i: number) => colors.chart[i % colors.chart.length]!

  const round = kind === 'donut' || kind === 'pie'
  const shown = round ? series.slice(0, 1) : series

  return (
    <View style={styles.figure}>
      {title ? <Text style={[styles.title, { color: colors.text }]}>{title}</Text> : null}
      <View onLayout={(event) => setWidth(Math.floor(event.nativeEvent.layout.width))}>
        {width > 0 ? (
          round ? (
            <Round kind={kind} labels={labels} values={shown[0]?.values ?? []} picked={picked} onPick={setPicked} color={color} colors={colors} format={format} />
          ) : (
            <Cartesian
              kind={kind}
              width={width}
              labels={labels}
              series={shown}
              picked={picked}
              onPick={setPicked}
              tick={(v) => compact.format(v)}
              color={color}
              colors={colors}
            />
          )
        ) : null}
      </View>
      {picked !== null && !round ? (
        <View style={[styles.tip, { backgroundColor: colors.bubble }]}>
          <Text style={[styles.tipLabel, { color: colors.text2 }]}>{labels[picked]}</Text>
          {shown.map((s, si) => (
            <View key={si} style={styles.legendItem}>
              <View style={[styles.swatch, { backgroundColor: color(si) }]} />
              {shown.length > 1 ? <Text style={[styles.legendText, { color: colors.text2 }]}>{s.name}</Text> : null}
              <Text style={[styles.tipValue, { color: colors.text }]}>{format(s.values[picked] ?? NaN)}</Text>
            </View>
          ))}
        </View>
      ) : null}
      {round || shown.length > 1 ? (
        <View style={styles.legend}>
          {(round ? labels : shown.map((s) => s.name)).map((name, i) => (
            <View key={i} style={[styles.legendItem, round && picked !== null && picked !== i && styles.dim]}>
              <View style={[styles.swatch, { backgroundColor: color(i) }]} />
              <Text style={[styles.legendText, { color: colors.text2 }]}>{name}</Text>
            </View>
          ))}
        </View>
      ) : null}
      {caption ? <Text style={[styles.caption, { color: colors.text3 }]}>{caption}</Text> : null}
    </View>
  )
}

/** Ticks as 1.2K; Hermes's Intl may not know compact notation, and then they are plain. */
function compactFormat(language: string): Intl.NumberFormat {
  try {
    return new Intl.NumberFormat(language, { notation: 'compact', maximumFractionDigits: 1 })
  } catch {
    return new Intl.NumberFormat(language, { maximumFractionDigits: 1 })
  }
}

interface CartesianProps {
  kind: 'line' | 'area' | 'bar'
  width: number
  labels: string[]
  series: ChartSeries[]
  picked: number | null
  onPick: (index: number | null) => void
  tick: (value: number) => string
  color: (i: number) => string
  colors: Colors
}

function Cartesian({ kind, width, labels, series, picked, onPick, tick, color, colors }: CartesianProps) {
  const n = labels.length
  const values = series.flatMap((s) => s.values.slice(0, n)).filter(Number.isFinite)
  const bars = kind === 'bar'
  const [min, max, step] = niceScale(
    bars || kind === 'area' ? Math.min(0, ...values) : Math.min(...values),
    Math.max(...values, bars ? 0 : -Infinity),
  )
  const fit = (left: number) => Math.max(width - left - PAD.right, 1) / Math.max(n, 1)
  const labelWidth = Math.max(...labels.map(textWidth), 0) + 14
  const slant = bars && labelWidth > fit(PAD.left)
  const shownLabels = slant ? labels.map((l) => clip(l, SLANT_CHARS)) : labels
  const firstReach = slant ? textWidth(shownLabels[0] ?? '') * SLANT_COS : 0
  const left = Math.max(PAD.left, Math.ceil(firstReach - fit(PAD.left) / 2 + 4))
  const plotW = Math.max(width - left - PAD.right, 1)
  const band = plotW / Math.max(n, 1)
  const every = bars ? 1 : Math.max(1, Math.ceil(n / Math.max(1, Math.floor(plotW / labelWidth))))
  const slantDepth = slant ? Math.max(...shownLabels.map(textWidth), 0) * SLANT_SIN + 12 : 0
  const height = PLOT_HEIGHT + PAD.top + (slant ? Math.ceil(slantDepth) + 14 : PAD.bottom)
  const x = (i: number) => (bars ? left + band * (i + 0.5) : left + (n > 1 ? (plotW * i) / (n - 1) : plotW / 2))
  const y = (v: number) => PAD.top + PLOT_HEIGHT - ((v - min) / (max - min || 1)) * PLOT_HEIGHT
  const ticks: number[] = []
  for (let v = min; v <= max + step / 2; v += step) ticks.push(Number(v.toPrecision(12)))
  const groupW = band * 0.64
  const barW = groupW / Math.max(series.length, 1)
  const tickStyle = { fill: colors.text3, fontSize: 11 }

  return (
    <Svg width={width} height={height}>
      {ticks.map((t) => (
        <G key={t}>
          <Line x1={left} x2={width - PAD.right} y1={y(t)} y2={y(t)} stroke={colors.line} strokeWidth={1} />
          <SvgText x={left - 8} y={y(t) + 4} textAnchor="end" {...tickStyle}>
            {tick(t)}
          </SvgText>
        </G>
      ))}
      {shownLabels.map((label, i) =>
        slant ? (
          <SvgText
            key={i}
            x={x(i)}
            y={PAD.top + PLOT_HEIGHT + 14}
            textAnchor="end"
            transform={`rotate(${-SLANT_DEGREES} ${x(i)} ${PAD.top + PLOT_HEIGHT + 14})`}
            {...tickStyle}
          >
            {label}
          </SvgText>
        ) : i % every === 0 ? (
          <SvgText key={i} x={x(i)} y={height - 8} textAnchor={bars || (i > 0 && i < n - 1) ? 'middle' : i === 0 ? 'start' : 'end'} {...tickStyle}>
            {label}
          </SvgText>
        ) : null,
      )}
      {picked !== null && !bars ? <Line x1={x(picked)} x2={x(picked)} y1={PAD.top} y2={PAD.top + PLOT_HEIGHT} stroke={colors.text3} strokeDasharray="3 3" /> : null}
      {bars
        ? series.map((s, si) =>
            s.values.slice(0, n).map((v, i) => {
              if (!Number.isFinite(v)) return null
              const top = y(Math.max(v, 0))
              const h = Math.max(Math.abs(y(v) - y(0)), 1)
              const bx = left + band * i + (band - groupW) / 2 + barW * si
              return (
                <Path
                  key={`${si}-${i}`}
                  d={roundedTop(bx + 1, top, Math.max(barW - 2, 1), h, Math.min(4, barW / 2))}
                  fill={color(si)}
                  opacity={picked !== null && picked !== i ? 0.35 : 1}
                />
              )
            }),
          )
        : series.map((s, si) => (
            <G key={si}>
              {kind === 'area' ? <Path d={areaPath(s.values.slice(0, n), x, y, y(Math.max(min, 0)))} fill={color(si)} opacity={0.18} /> : null}
              <Path d={linePath(s.values.slice(0, n), x, y)} stroke={color(si)} strokeWidth={2} fill="none" strokeLinejoin="round" />
              {n <= 24
                ? s.values.slice(0, n).map((v, i) =>
                    Number.isFinite(v) ? <Circle key={i} cx={x(i)} cy={y(v)} r={picked === i ? 5 : 3.5} fill={color(si)} /> : null,
                  )
                : null}
            </G>
          ))}
      {/* One tap target per label, across the plot's height. */}
      {labels.map((_, i) => {
        const from = bars ? left + band * i : n > 1 ? x(i) - plotW / (n - 1) / 2 : left
        const span = bars ? band : n > 1 ? plotW / (n - 1) : plotW
        return (
          <Rect
            key={`hit-${i}`}
            x={Math.max(left, from)}
            y={PAD.top}
            width={span}
            height={PLOT_HEIGHT}
            fill="transparent"
            onPress={() => onPick(picked === i ? null : i)}
          />
        )
      })}
    </Svg>
  )
}

interface RoundProps {
  kind: 'donut' | 'pie'
  labels: string[]
  values: number[]
  picked: number | null
  onPick: (index: number | null) => void
  color: (i: number) => string
  colors: Colors
  format: (value: number) => string
}

function Round({ kind, labels, values, picked, onPick, color, colors, format }: RoundProps) {
  const size = 220
  const r = size / 2
  const inner = kind === 'donut' ? r * 0.56 : 0
  const slices = labels.map((_, i) => Math.max(values[i] ?? 0, 0)).map((v) => (Number.isFinite(v) ? v : 0))
  const total = slices.reduce((a, b) => a + b, 0) || 1
  const start = -Math.PI / 2
  const arcs = slices.map((v, i) => {
    const before = slices.slice(0, i).reduce((a, b) => a + b, 0)
    const from = start + (before / total) * Math.PI * 2
    return { i, from, to: from + (v / total) * Math.PI * 2, value: v }
  })
  const focus = picked !== null ? arcs[picked] : null
  return (
    <View style={styles.round}>
      <Svg width={size} height={size} viewBox={`0 0 ${size} ${size}`}>
        {arcs.map((a) =>
          a.value > 0 ? (
            <Path
              key={a.i}
              d={slicePath(r, r, picked === a.i ? r : r - 4, inner, a.from, a.to)}
              fill={color(a.i)}
              opacity={picked !== null && picked !== a.i ? 0.35 : 1}
              onPress={() => onPick(picked === a.i ? null : a.i)}
            />
          ) : null,
        )}
      </Svg>
      {focus ? (
        <View style={kind === 'donut' ? styles.center : styles.under} pointerEvents="none">
          <Text style={[styles.centerValue, { color: colors.text }]}>{Math.round((focus.value / total) * 100)}%</Text>
          <Text style={[styles.centerLabel, { color: colors.text2 }]} numberOfLines={1}>
            {labels[focus.i]}
          </Text>
          <Text style={[styles.centerLabel, { color: colors.text3 }]}>{format(focus.value)}</Text>
        </View>
      ) : null}
    </View>
  )
}

const styles = StyleSheet.create({
  figure: { gap: 8 },
  title: { fontSize: 15, fontWeight: '600' },
  tip: { borderRadius: 12, padding: 10, gap: 4 },
  tipLabel: { fontSize: 12 },
  tipValue: { fontSize: 14, fontWeight: '500' },
  legend: { flexDirection: 'row', flexWrap: 'wrap', gap: 12 },
  legendItem: { flexDirection: 'row', alignItems: 'center', gap: 6 },
  legendText: { fontSize: 12 },
  swatch: { width: 10, height: 10, borderRadius: 3 },
  dim: { opacity: 0.4 },
  caption: { fontSize: 12, lineHeight: 17 },
  round: { alignItems: 'center' },
  center: { position: 'absolute', top: 0, bottom: 0, width: 120, alignItems: 'center', justifyContent: 'center' },
  under: { alignItems: 'center', marginTop: 8 },
  centerValue: { fontSize: 22, fontWeight: '600' },
  centerLabel: { fontSize: 12 },
})
