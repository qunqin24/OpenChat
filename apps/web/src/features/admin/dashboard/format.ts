import { formatPercent } from '@/features/admin/usage-logs/format'

/** How a change reads: green is better, red is worse, grey is neither. */
export type Tone = 'good' | 'bad' | 'neutral'

export type Delta = { text: string; tone: Tone }

export const deltaClass: Record<Tone, string> = {
  good: 'text-emerald-600 dark:text-emerald-400',
  bad: 'text-destructive',
  neutral: 'text-muted-foreground',
}

/** Which way is better for a figure; `none` where more is neither good nor bad. */
export type Direction = 'up' | 'down' | 'none'

function toneOf(change: number, better: Direction): Tone {
  if (better === 'none' || change === 0) return 'neutral'
  return (change > 0) === (better === 'up') ? 'good' : 'bad'
}

const arrow = (change: number) => (change > 0 ? '↑' : change < 0 ? '↓' : '')

/**
 * The relative change of an amount against the window before. None where the
 * window before had nothing: "up from zero" is not a percentage.
 */
export function relativeDelta(current: number, previous: number, better: Direction): Delta | undefined {
  if (previous <= 0) return undefined
  const change = (current - previous) / previous
  const rounded = Math.round(Math.abs(change) * 1000) / 10
  if (rounded === 0) return { text: '0%', tone: 'neutral' }
  return { text: `${arrow(change)} ${rounded.toLocaleString('en-US')}%`, tone: toneOf(change, better) }
}

/** The change of a rate in percentage points: 62% to 65% is `↑ 3 pt`, not 4.8%. */
export function pointDelta(current: number | undefined, previous: number | undefined, better: Direction): Delta | undefined {
  if (current == null || previous == null) return undefined
  const change = current - previous
  const points = Math.round(Math.abs(change) * 1000) / 10
  if (points === 0) return { text: '0 pt', tone: 'neutral' }
  return { text: `${arrow(change)} ${points.toLocaleString('en-US')} pt`, tone: toneOf(change, better) }
}

/** Errors as a share of requests; undefined with no requests. */
export function errorRate(errors: number, requests: number): number | undefined {
  return requests > 0 ? errors / requests : undefined
}

/** A failure share in red once it is a real one, amber while it is a few. */
export function errorRateClass(rate: number | undefined): string {
  if (rate == null || rate === 0) return 'text-muted-foreground'
  return rate >= 0.05 ? 'text-destructive font-medium' : 'text-amber-600 dark:text-amber-400'
}

export function formatRate(rate: number | undefined | null): string {
  return rate == null ? '—' : formatPercent(rate)
}
