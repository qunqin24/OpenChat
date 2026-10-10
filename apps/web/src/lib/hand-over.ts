/**
 * Handing a finished run over from the store to the query cache.
 *
 * A run's rows are shown from the chat store (`live`) while it streams, and from
 * the message list once it is over. The live copy may be let go only when the
 * list is *known* to hold the rows: letting it go because a refresh was
 * attempted - and failed - makes the finished answer vanish.
 */

export interface Refresher {
  /** Fetches the newest page and splices it in; rejects if it could not. */
  refreshNewest: () => Promise<unknown>
  /** The plain refetch of every page, as the fallback; rejects if it could not. */
  refetchAll: () => Promise<unknown>
}

/** True only when the message list was brought up to date. */
export async function refreshConfirmed({ refreshNewest, refetchAll }: Refresher): Promise<boolean> {
  try {
    await refreshNewest()
    return true
  } catch {
    // Fall through to the whole refetch.
  }
  try {
    await refetchAll()
    return true
  } catch {
    return false
  }
}

/** How long to wait before asking again after a refresh that failed, in ms. */
export const REFRESH_RETRY_DELAYS = [2000, 5000, 10_000]

export interface SettleOptions {
  /** One attempt at bringing the list up to date; true when it was. */
  refresh: () => Promise<boolean>
  /** Drops the live copy of the run. */
  settle: (runId: string) => void
  /** The run whose rows the list should hold; none means there is nothing to drop. */
  runId?: string
  wait: (ms: number) => Promise<void>
  cancelled: () => boolean
  delays?: number[]
}

/**
 * Refreshes the list and, once that is confirmed, drops the run's live copy.
 * A refresh that fails leaves the live copy where it is - the answer stays on
 * screen - and is tried again after each delay; a later success settles it.
 * Resolves true once the list is confirmed up to date.
 */
export async function refreshThenSettle({
  refresh,
  settle,
  runId,
  wait,
  cancelled,
  delays = REFRESH_RETRY_DELAYS,
}: SettleOptions): Promise<boolean> {
  for (let attempt = 0; ; attempt++) {
    if (await refresh()) {
      if (runId) settle(runId)
      return true
    }
    const delay = delays[attempt]
    if (cancelled() || delay === undefined) return false
    await wait(delay)
    if (cancelled()) return false
  }
}
