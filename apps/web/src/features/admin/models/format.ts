/**
 * Compact renderings for the numbers on a model card. A context window reads as
 * `1M`, not `1000000`: on a card the exact figure is noise, and a 7-digit number
 * pushes the layout around for no gain.
 *
 * `K`/`M` are left untranslated on purpose — they are the units the model
 * documentation itself uses, in both languages this app ships.
 */

function trim(value: number): string {
  return Number(value.toFixed(value < 10 ? 1 : 0)).toString()
}

export function formatTokenLimit(value: number): string {
  if (value >= 1_000_000) return `${trim(value / 1_000_000)}M`
  if (value >= 1_000) return `${trim(value / 1_000)}K`
  return String(value)
}

/**
 * Prices arrive as USD per million tokens and span three orders of magnitude —
 * $0.02 next to $75 — so the precision has to follow the value rather than sit
 * at a fixed two places.
 */
export function formatCost(value: number): string {
  return Number(value.toFixed(value >= 1 ? 2 : 3)).toString()
}

export function formatSyncedAt(iso: string, locale: string): string {
  const date = new Date(iso)
  if (Number.isNaN(date.getTime())) return iso
  return new Intl.DateTimeFormat(locale, { dateStyle: 'medium', timeStyle: 'short' }).format(date)
}
