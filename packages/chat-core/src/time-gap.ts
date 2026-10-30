import type { Turn } from './transcript'

/**
 * The pause after which a message of the person's is shown with the time it
 * was sent, and the model is told the time (agent.TimeGap, BuildContextIn):
 * taking a conversation up again later is something both should see.
 */
export const TIME_GAP_MS = 60 * 60 * 1000

/** When a turn was written: its message, or a reply's last response. */
function turnTime(turn: Turn): number | null {
  const at = turn.kind === 'assistant' ? turn.responses.at(-1)?.createdAt : turn.message.createdAt
  const time = at ? Date.parse(at) : NaN
  return Number.isNaN(time) ? null : time
}

/**
 * The person's turns that come TIME_GAP_MS or more after the turn before them,
 * by key, with when they were sent: where the page draws a time above them.
 */
export function timeGaps(turns: Turn[]): Map<string, number> {
  const gaps = new Map<string, number>()
  let last: number | null = null
  for (const turn of turns) {
    const at = turnTime(turn)
    if (turn.kind === 'user' && at !== null && last !== null && at - last >= TIME_GAP_MS) gaps.set(turn.key, at)
    if (at !== null) last = at
  }
  return gaps
}

export interface GapWords {
  today: string
  yesterday: string
}

/**
 * The time shown above a message after a pause: "今天 10:44", "昨天 21:05",
 * "10月28日 10:44" within the year, the year too before it.
 */
export function gapLabel(at: number, now: number, locale: string, words: GapWords): string {
  const date = new Date(at)
  const time = new Intl.DateTimeFormat(locale, { hour: '2-digit', minute: '2-digit', hour12: false }).format(date)
  const day = (d: Date) => new Date(d.getFullYear(), d.getMonth(), d.getDate()).getTime()
  const today = day(new Date(now))
  const that = day(date)
  if (that === today) return `${words.today} ${time}`
  if (today - that <= 26 * 60 * 60 * 1000 && today > that) return `${words.yesterday} ${time}`
  const sameYear = date.getFullYear() === new Date(now).getFullYear()
  const dayText = new Intl.DateTimeFormat(locale, sameYear ? { month: 'long', day: 'numeric' } : { year: 'numeric', month: 'long', day: 'numeric' }).format(date)
  return `${dayText} ${time}`
}
