// How full a model's context window is, as both apps show it beside the
// composer: the web's ContextMeter, the app's ContextMeter. The figure is what
// the conversation's latest request took (GET /conversations/{id}/context),
// against the window of the model chosen now, which the next request goes to.

/** 32k, 258k, 1.2M: the window sizes people know models by. */
export function compactTokens(tokens: number): string {
  if (tokens >= 1_000_000) return `${+(tokens / 1_000_000).toFixed(1)}M`
  if (tokens >= 1_000) return `${Math.round(tokens / 1_000)}k`
  return String(tokens)
}

export interface ContextGauge {
  /** How much of the window is used, 0 to 1 (more than the window counts as all of it). */
  share: number
  percent: number
  /** Amber past 80%, red past 95%. */
  level: 'high' | 'full' | null
  used: string
  limit: string
}

/** Null until something has been answered, or for a model whose window is unknown: nothing to show. */
export function contextGauge(used: number | null | undefined, limit: number | null | undefined): ContextGauge | null {
  if (used == null || !limit) return null
  const share = Math.min(used / limit, 1)
  return {
    share,
    percent: Math.round(share * 100),
    level: share >= 0.95 ? 'full' : share >= 0.8 ? 'high' : null,
    used: compactTokens(used),
    limit: compactTokens(limit),
  }
}
