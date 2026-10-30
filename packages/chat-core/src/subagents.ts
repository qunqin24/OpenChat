import type { Message, Subagent, ToolCallPart, ToolResultPart } from '@openchat/api-client'

/**
 * Sub-agents (子智能体) as the chat draws them: one line in a reply when the
 * main agent launches some, one line where a report is delivered, a small
 * summary in the corner, and the side panel (docs/design/sub-agents.md §7).
 */

export const SUBAGENT_TOOL = 'subagent'
export const SUBAGENT_MESSAGE_TOOL = 'subagent_message'
export const SUBAGENT_STOP_TOOL = 'subagent_stop'

export type ReportState = 'completed' | 'failed' | 'cancelled'

/** One report a delivered row carries, from its `<subagent-report>` tag. */
export interface ReportNotice {
  id: string
  name: string
  state: ReportState
  durationMs: number
}

const REPORT_TAG = /<subagent-report\s+([^>]*)>/g
const ATTRIBUTE = /(\w+)="([^"]*)"/g

/**
 * The reports a delivered row (`origin: 'subagent'`) carries, in order. Only
 * the tags are read: the text of a report is the main agent's to read, and the
 * person reads it in the sub-agent's own view.
 */
export function parseReports(content: string): ReportNotice[] {
  const out: ReportNotice[] = []
  for (const tag of content.matchAll(REPORT_TAG)) {
    const attrs: Record<string, string> = {}
    for (const [, key, value] of (tag[1] ?? '').matchAll(ATTRIBUTE)) attrs[key!] = value!
    if (!attrs.id) continue
    const state = attrs.state === 'failed' || attrs.state === 'cancelled' ? attrs.state : 'completed'
    out.push({ id: attrs.id, name: attrs.name ?? '', state, durationMs: parseDuration(attrs.duration ?? '') })
  }
  return out
}

/** A Go duration as the server writes it ("3m12s", "1h2m0s", "850ms"), in milliseconds. */
export function parseDuration(text: string): number {
  let total = 0
  for (const [, amount, unit] of text.matchAll(/(\d+(?:\.\d+)?)(h|ms|m|s)/g)) {
    const n = Number(amount)
    total += unit === 'h' ? n * 3_600_000 : unit === 'm' ? n * 60_000 : unit === 's' ? n * 1000 : n
  }
  return Math.round(total)
}

/** Whether a row is a delivered report rather than anything the person wrote. */
export function isReportRow(message: Pick<Message, 'role' | 'origin'>): boolean {
  return message.role === 'user' && message.origin === 'subagent'
}

/** How long, as the lines say it: whole seconds, minutes and seconds past a minute. */
export function durationParts(ms: number): { minutes: number; seconds: number } {
  const total = Math.max(0, Math.round(ms / 1000))
  return { minutes: Math.floor(total / 60), seconds: total % 60 }
}

/**
 * Names joined as a sentence lists them: 「A、B和C」 in Chinese, "A, B and C"
 * in English.
 */
export function joinNames(names: string[], language: string): string {
  const zh = language.startsWith('zh')
  if (names.length <= 1) return names[0] ?? ''
  const head = names.slice(0, -1)
  const tail = names.at(-1)!
  return zh ? `${head.join('、')}和${tail}` : `${head.join(', ')} and ${tail}`
}

/** How many colours a sub-agent's mark is drawn in (`--n-agent-1` … in subagents.css). */
export const AGENT_COLORS = 8

/**
 * A sub-agent's colour, as an index into the palette: fixed by its id, so the
 * same sub-agent looks the same in the reply, the summary and the panel.
 */
export function colorOf(id: string): number {
  let h = 2166136261
  for (let i = 0; i < id.length; i++) {
    h ^= id.charCodeAt(i)
    h = Math.imul(h, 16777619)
  }
  const n = h >>> 0
  return n % AGENT_COLORS
}

/** What the interface is shown of a `subagent` call's result (api SubagentToolDetails). */
export interface LaunchDetails {
  id: string
  name: string
  background: boolean
  state: 'running' | ReportState
  durationMs?: number
}

export function launchDetailsOf(result: ToolResultPart | undefined): LaunchDetails | null {
  const d = result?.details as Partial<LaunchDetails> | undefined
  if (!d || typeof d.id !== 'string' || typeof d.name !== 'string') return null
  const state = d.state === 'completed' || d.state === 'failed' || d.state === 'cancelled' ? d.state : 'running'
  return { id: d.id, name: d.name, background: Boolean(d.background), state, durationMs: d.durationMs }
}

/** One sub-agent a reply launched, as its line draws it. */
export interface Launch {
  callId: string
  /** The sub-agent's id once the call says it; until then the call's, for the avatar. */
  id: string
  name: string
  /** Known once the call has a result. */
  launched: boolean
  background: boolean
  /** A foreground one still being waited for. */
  waiting: boolean
  /** A foreground one that came back with its report: its state and time. */
  done?: { state: ReportState; durationMs: number }
  /** The call failed: refused (a limit), or the run stopped before it. */
  failed: boolean
}

/**
 * The sub-agents one response launched - its `subagent` calls, in order, with
 * what their results say. A call still being written has no name yet.
 */
export function launchesOf(parts: { type: string }[], results: Record<string, ToolResultPart>): Launch[] {
  const out: Launch[] = []
  for (const part of parts) {
    if (part.type !== 'tool_call') continue
    const call = part as ToolCallPart
    if (call.name !== SUBAGENT_TOOL) continue
    const args = (call.arguments ?? {}) as { name?: unknown; background?: unknown }
    const result = results[call.id]
    const details = launchDetailsOf(result)
    const background = details?.background ?? args.background === true
    out.push({
      callId: call.id,
      id: details?.id ?? call.id,
      name: details?.name ?? (typeof args.name === 'string' ? args.name.trim() : ''),
      launched: Boolean(details),
      background,
      waiting: !result && !background,
      done: details && !details.background && details.state !== 'running'
        ? { state: details.state, durationMs: details.durationMs ?? 0 }
        : undefined,
      failed: result?.status !== undefined && result.status !== 'succeeded',
    })
  }
  return out
}

/** How a conversation's sub-agents stand, for the corner and the panel's groups. */
export function tally(list: Subagent[]): { active: Subagent[]; running: Subagent[]; queued: Subagent[]; finished: Subagent[] } {
  const working = (a: Subagent) => a.state === 'running' || a.state === 'queued'
  // Newest first in each group: what was just launched or just ended is on
  // top. The working group is ordered by launch, so a queued one that starts
  // keeps its row where it is.
  const byRecent = (a: Subagent, b: Subagent) => (b.finishedAt ?? b.startedAt).localeCompare(a.finishedAt ?? a.startedAt)
  const active = list.filter(working).sort((a, b) => b.createdAt.localeCompare(a.createdAt))
  return {
    active,
    running: active.filter((a) => a.state === 'running'),
    queued: active.filter((a) => a.state === 'queued'),
    finished: list.filter((a) => !working(a)).sort(byRecent),
  }
}

/**
 * The names of a launch line, as started or queued: a foreground sub-agent
 * waiting for a slot is listed `queued` from its launch (found by the call
 * that launched it), and the line must not say it started.
 */
export function launchNames(launches: Launch[], list: Subagent[] | undefined): { started: string[]; queued: string[] } {
  const started: string[] = []
  const queued: string[] = []
  for (const launch of launches) {
    if (!launch.name) continue
    const known = list?.find((a) => a.callId === launch.callId || a.id === launch.id)
    if (known?.state === 'queued') queued.push(launch.name)
    else started.push(launch.name)
  }
  return { started, queued }
}

/**
 * When the corner summary shows: the conversation has sub-agents, and either
 * one is working or the panel was opened since (`seen`) - kept up to look back
 * at until the panel is closed or the page left.
 */
export function showSummary(list: Subagent[], seen: boolean): boolean {
  if (list.length === 0) return false
  return seen || list.some((a) => a.state === 'running' || a.state === 'queued')
}

/** The relative time a panel row shows: "just now", minutes, hours, days. */
export function relativeAge(at: string, now: number): { unit: 'now' | 'minute' | 'hour' | 'day'; count: number } {
  const seconds = Math.max(0, (now - Date.parse(at)) / 1000)
  if (seconds < 60) return { unit: 'now', count: 0 }
  if (seconds < 3600) return { unit: 'minute', count: Math.floor(seconds / 60) }
  if (seconds < 86400) return { unit: 'hour', count: Math.floor(seconds / 3600) }
  return { unit: 'day', count: Math.floor(seconds / 86400) }
}

/** How a launch line stands, which its words are made from (launchProgress). */
export type LaunchProgress =
  /** Nothing has ended yet: the names, as started or queued. */
  | { kind: 'names'; started: string[]; queued: string[] }
  /** Some have ended, some are still at it; `done` counts those that completed. */
  | { kind: 'progress'; total: number; queued: number; done: number; stopped: number; failed: number }
  /** All have ended: how long it took, from the first launch to the last end. */
  | { kind: 'finished'; total: number; durationMs: number; stopped: number; failed: number; single?: { name: string; state: ReportState } }

/**
 * How the sub-agents one response launched stand, for the one line the reply
 * shows of them - kept up to date in place, with no line per sub-agent: the
 * names while none has ended, then counts, then the total time once all have.
 * Each is found in the conversation's list by the call that launched it; one
 * not listed yet reads as its call says.
 */
export function launchProgress(launches: Launch[], list: Subagent[] | undefined): LaunchProgress {
  const shown = launches.filter((l) => !l.failed)
  const agents = shown.map((launch) => ({ launch, agent: list?.find((a) => a.callId === launch.callId || a.id === launch.id) }))
  const stateOf = ({ launch, agent }: (typeof agents)[number]) =>
    agent?.state ?? (launch.done ? launch.done.state : 'running')
  const states = agents.map(stateOf)
  const ended = states.filter((s) => s === 'completed' || s === 'failed' || s === 'cancelled')
  const stopped = states.filter((s) => s === 'cancelled').length
  const failed = states.filter((s) => s === 'failed').length
  if (ended.length === 0) return { kind: 'names', ...launchNames(shown, list) }
  if (ended.length < states.length) {
    const done = states.filter((s) => s === 'completed').length
    return { kind: 'progress', total: states.length, queued: states.filter((s) => s === 'queued').length, done, stopped, failed }
  }
  // From the first launch to the last end; what the calls said when the list is not in.
  const starts = agents.map(({ agent }) => agent?.createdAt).filter((at): at is string => Boolean(at))
  const ends = agents.map(({ agent }) => agent?.finishedAt).filter((at): at is string => Boolean(at))
  const durationMs =
    starts.length > 0 && ends.length > 0
      ? Math.max(0, Math.max(...ends.map(Date.parse)) - Math.min(...starts.map(Date.parse)))
      : Math.max(0, ...agents.map(({ launch, agent }) => agent?.durationMs ?? launch.done?.durationMs ?? 0))
  const single = agents.length === 1 ? { name: agents[0]!.launch.name, state: states[0] as ReportState } : undefined
  return { kind: 'finished', total: states.length, durationMs, stopped, failed, single }
}
