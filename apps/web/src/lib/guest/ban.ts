/**
 * What a response says about a ban on this guest's address: `undefined` when it
 * is not the server's `guest_banned` refusal, else when the ban ends (an ISO
 * time) or `null` when it never does.
 */
export function bannedUntil(body: unknown): string | null | undefined {
  if (typeof body !== 'object' || body === null || (body as { code?: unknown }).code !== 'guest_banned') return undefined
  const details = (body as { details?: unknown }).details
  const until = typeof details === 'object' && details !== null ? (details as { until?: unknown }).until : undefined
  return typeof until === 'string' && until !== '' ? until : null
}
