import type { UsageLog } from './use-usage-logs'

/** `2026-10-10 15:21:20`, local time: a log is read for when, not for how long ago. */
export function formatLogTime(iso: string): string {
  const date = new Date(iso)
  if (Number.isNaN(date.getTime())) return iso
  const pad = (n: number) => String(n).padStart(2, '0')
  return (
    `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())} ` +
    `${pad(date.getHours())}:${pad(date.getMinutes())}:${pad(date.getSeconds())}`
  )
}

export function formatCount(value: number): string {
  return value.toLocaleString('en-US')
}

/** `1.2M`, `34.5K`, `812`: a figure that has to fit a card, not be summed up. */
export function formatCompact(value: number): string {
  return new Intl.NumberFormat('en-US', { notation: 'compact', maximumFractionDigits: 1 }).format(value)
}

/** A share as a percentage: `62.4%`, `100%`, and `<0.1%` rather than a false zero. */
export function formatPercent(rate: number): string {
  if (rate <= 0) return '0%'
  if (rate < 0.001) return '<0.1%'
  return `${(rate * 100).toFixed(1).replace(/\.0$/, '')}%`
}

/**
 * The share of a request's prompt tokens that were read from the cache.
 * `inputTokens` excludes the cached part, so the prompt is all three. Undefined
 * where nothing was sent -- there is no rate, which is not the same as 0%.
 */
export function cacheHitRate(log: {
  inputTokens: number
  cacheReadTokens: number
  cacheWriteTokens: number
}): number | undefined {
  const prompt = log.inputTokens + log.cacheReadTokens + log.cacheWriteTokens
  return prompt > 0 ? log.cacheReadTokens / prompt : undefined
}

/**
 * Dollars, with the precision following the amount: one request often costs a
 * fraction of a cent, and `$0.00` for it would say nothing.
 */
export function formatUsd(value: number): string {
  if (value === 0) return '$0'
  if (value < 0.000001) return '<$0.000001'
  const places = value >= 1 ? 2 : value >= 0.01 ? 4 : 6
  return `$${value.toFixed(places).replace(/(\.\d*?[1-9])0+$|\.0+$/, '$1')}`
}

/** `420ms` under a second, `2.4s` above it, `1m 05s` past a minute. */
export function formatDuration(ms: number): string {
  if (ms < 1000) return `${Math.round(ms)}ms`
  const seconds = ms / 1000
  if (seconds < 60) return `${seconds.toFixed(seconds < 10 ? 2 : 1)}s`
  const minutes = Math.floor(seconds / 60)
  return `${minutes}m ${String(Math.round(seconds - minutes * 60)).padStart(2, '0')}s`
}

/**
 * Output tokens per second of generation: the time after the first token, so
 * the wait for it does not count against the model's speed. Not computable
 * without a first token, without output, or when it all arrived at once.
 */
export function outputSpeed(log: UsageLog): number | undefined {
  if (log.firstTokenMs == null || log.outputTokens <= 0) return undefined
  const generating = log.durationMs - log.firstTokenMs
  if (generating <= 0) return undefined
  return log.outputTokens / (generating / 1000)
}

/**
 * A failed request the provider turned down before any time was spent on it
 * (a bad key, a malformed request) is recorded with a duration of 0. That is
 * not a fast request, and "0ms" in the column would read as one.
 */
export function rejectedAtOnce(log: Pick<UsageLog, 'status' | 'durationMs'>): boolean {
  return log.status === 'error' && log.durationMs <= 0
}

export type Tone = 'fast' | 'medium' | 'slow'

/**
 * Green, amber, red. Time to first token: up to 1 s is fast, up to 3 s is
 * acceptable, beyond is slow. Total time: up to 5 s, up to 15 s, beyond.
 */
export function firstTokenTone(ms: number): Tone {
  return ms <= 1000 ? 'fast' : ms <= 3000 ? 'medium' : 'slow'
}

export function durationTone(ms: number): Tone {
  return ms <= 5000 ? 'fast' : ms <= 15000 ? 'medium' : 'slow'
}

export const toneClass: Record<Tone, string> = {
  fast: 'bg-emerald-500/10 text-emerald-700 dark:text-emerald-400',
  medium: 'bg-amber-500/15 text-amber-700 dark:text-amber-400',
  slow: 'bg-red-500/10 text-red-700 dark:text-red-400',
}

/** The codes the chat has a sentence for (`chat.error.*`). */
export const KNOWN_ERROR_CODES = [
  'no_model',
  'model_error',
  'model_auth',
  'model_rate_limited',
  'context_too_long',
  'internal',
] as const

export type KnownErrorCode = (typeof KNOWN_ERROR_CODES)[number]

export function knownErrorCode(code?: string | null): KnownErrorCode | undefined {
  return KNOWN_ERROR_CODES.find((known) => known === code)
}
