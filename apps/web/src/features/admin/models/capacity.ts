import type { CapacityPeriod } from './use-catalog'

/** The most decimals a cap is kept with; the server refuses more. */
const MAX_DECIMALS = 6
/** The largest cap the server accepts, in USD. */
const MAX_CAPACITY = 1_000_000_000

export type CapacityInput = { ok: true; value: number | null } | { ok: false }

/**
 * Reads the cap field of the settings form. Empty is no cap; otherwise a
 * positive amount in dollars with at most six decimals, which is what the
 * server stores exactly. Typed text, not a number input, so "0.30" stays as it
 * was written and a comma or an exponent is refused rather than guessed at.
 */
export function parseCapacity(raw: string): CapacityInput {
  const text = raw.trim()
  if (text === '') return { ok: true, value: null }
  if (!new RegExp(`^\\d+(\\.\\d{1,${MAX_DECIMALS}})?$`).test(text)) return { ok: false }
  const value = Number(text)
  return value > 0 && value <= MAX_CAPACITY ? { ok: true, value } : { ok: false }
}

/** A cap as the field shows it: no trailing zeros, nothing for no cap. */
export function capacityText(value: number | null | undefined): string {
  return value == null ? '' : String(Number(value.toFixed(MAX_DECIMALS)))
}

/**
 * Dollars for a spend or a cap. Cents from a cent up, then as many decimals as
 * the amount needs: a cap of $0.0005 must not read as $0.00, and a spend of
 * $0.0003 must not read as nothing. `compact` drops the cents of a whole
 * amount ($20, not $20.00), for the small space on a card.
 */
export function formatMoney(value: number, compact = false): string {
  if (value === 0) return compact ? '$0' : '$0.00'
  if (value >= 0.01) {
    const text = value.toLocaleString('en-US', { minimumFractionDigits: 2, maximumFractionDigits: 2 })
    return `$${compact ? text.replace(/\.00$/, '') : text}`
  }
  return `$${Number(value.toFixed(MAX_DECIMALS)).toString()}`
}

/** How much of the cap is used, 0 to 1, for a bar. */
export function usedRatio(spent: number, capacity: number): number {
  if (!(capacity > 0)) return 0
  return Math.min(1, Math.max(0, spent / capacity))
}

/** The periods in the order they are offered. */
export const PERIODS = ['total', 'monthly', 'daily'] as const satisfies readonly CapacityPeriod[]
