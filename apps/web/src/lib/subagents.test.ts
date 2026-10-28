import type { Subagent } from '@openchat/api-client'
import { describe, expect, it } from 'vitest'

import {
  AGENT_COLORS,
  colorOf,
  durationParts,
  joinNames,
  launchNames,
  launchProgress,
  launchesOf,
  parseDuration,
  parseReports,
  relativeAge,
  showSummary,
  tally,
} from './subagents'
import { assistant, id, liveRun, message, run, toolCallRow, toolResultRow, user } from './testing'
import { buildTurns, regenerable } from './transcript'

const report = (name: string, agent: string, state = 'completed', duration = '3m12s') =>
  `<subagent-report name="${name}" id="${id(agent)}" state="${state}" duration="${duration}">\n(notice)\n\nThe findings.\n</subagent-report>`

const reportRow = (name: string, content: string, runId?: string) =>
  message({ id: id(name), role: 'user', origin: 'subagent', content, parts: [{ type: 'text', text: content }], ...(runId ? { runId } : {}) })

function agent(over: Partial<Subagent> & Pick<Subagent, 'id'>): Subagent {
  return {
    name: 'a',
    model: 'm',
    state: 'completed',
    background: true,
    stoppedByUser: false,
    toolCalls: 0,
    createdAt: '2026-10-27T00:00:00Z',
    startedAt: '2026-10-27T00:00:00Z',
    ...over,
  }
}

describe('parseReports', () => {
  it('reads every report tag of a delivered row, in order', () => {
    const content = `${report('价格对比', 'p')}\n\n${report('资料调研', 'r', 'cancelled', '45s')}`
    expect(parseReports(content)).toEqual([
      { id: id('p'), name: '价格对比', state: 'completed', durationMs: 192_000 },
      { id: id('r'), name: '资料调研', state: 'cancelled', durationMs: 45_000 },
    ])
  })

  it('ignores a tag without an id and reads an unknown state as completed', () => {
    expect(parseReports('<subagent-report name="x">')).toEqual([])
    expect(parseReports(`<subagent-report id="${id('a')}" state="odd" duration="1s">`)[0]?.state).toBe('completed')
  })
})

describe('durations', () => {
  it('reads Go durations', () => {
    expect(parseDuration('3m12s')).toBe(192_000)
    expect(parseDuration('1h2m0s')).toBe(3_720_000)
    expect(parseDuration('850ms')).toBe(850)
    expect(parseDuration('0s')).toBe(0)
    expect(parseDuration('')).toBe(0)
  })

  it('splits into minutes and seconds', () => {
    expect(durationParts(192_400)).toEqual({ minutes: 3, seconds: 12 })
    expect(durationParts(59_600)).toEqual({ minutes: 1, seconds: 0 })
  })
})

describe('joinNames', () => {
  it('joins as Chinese and English sentences do', () => {
    expect(joinNames(['资料调研', '价格对比', '竞品梳理'], 'zh')).toBe('资料调研、价格对比和竞品梳理')
    expect(joinNames(['A', 'B'], 'zh-CN')).toBe('A和B')
    expect(joinNames(['A', 'B', 'C'], 'en')).toBe('A, B and C')
    expect(joinNames(['A'], 'en')).toBe('A')
    expect(joinNames([], 'en')).toBe('')
  })
})

describe('colorOf', () => {
  it('is fixed by the id, within the palette, and varies between ids', () => {
    expect(colorOf(id('a'))).toBe(colorOf(id('a')))
    const seen = new Set(Array.from({ length: 40 }, (_, i) => colorOf(id(`x${i}`))))
    expect([...seen].every((c) => c >= 0 && c < AGENT_COLORS)).toBe(true)
    expect(seen.size).toBeGreaterThan(4)
  })
})

describe('launchesOf', () => {
  it('reads the launches of a response with what their results say', () => {
    const call = (callId: string, name: string, background: boolean) => ({
      type: 'tool_call' as const,
      id: callId,
      name: 'subagent',
      rawArguments: '{}',
      arguments: { name, task: 't', background },
    })
    const parts = [call('c1', '调研', true), call('c2', '对比', false), call('c3', '超限', true), { type: 'text' as const, text: 'x' }]
    const results = {
      c1: { type: 'tool_result' as const, callId: 'c1', name: 'subagent', content: '', status: 'succeeded' as const, details: { id: id('s1'), name: '调研', background: true, state: 'running' } },
      c3: { type: 'tool_result' as const, callId: 'c3', name: 'subagent', content: 'too many', status: 'failed' as const },
    }
    const launches = launchesOf(parts, results)
    expect(launches.map((l) => [l.name, l.id, l.launched, l.waiting, l.failed])).toEqual([
      ['调研', id('s1'), true, false, false],
      ['对比', 'c2', false, true, false],
      ['超限', 'c3', false, false, true],
    ])
  })

  it('says how a foreground sub-agent came back', () => {
    const parts = [{ type: 'tool_call' as const, id: 'c', name: 'subagent', rawArguments: '{}', arguments: { name: 'n', background: false } }]
    const results = {
      c: { type: 'tool_result' as const, callId: 'c', name: 'subagent', content: '', status: 'succeeded' as const, details: { id: id('s'), name: 'n', background: false, state: 'failed', durationMs: 4000 } },
    }
    expect(launchesOf(parts, results)[0]?.done).toEqual({ state: 'failed', durationMs: 4000 })
  })

  it('has no name for a call still being written', () => {
    const parts = [{ type: 'tool_call' as const, id: 'c', name: 'subagent', rawArguments: '{"na' }]
    expect(launchesOf(parts, {})[0]).toMatchObject({ name: '', waiting: true, launched: false })
  })
})

describe('launchNames', () => {
  it('says which launched ones are queued rather than started', () => {
    const launches = launchesOf(
      ['c1', 'c2', 'c3'].map((c, i) => ({ type: 'tool_call', id: c, name: 'subagent', rawArguments: '{}', arguments: { name: `n${i}`, background: false } })),
      {},
    )
    const list = [agent({ id: id('s2'), callId: 'c2', state: 'queued' }), agent({ id: id('s1'), callId: 'c1', state: 'running' })]
    expect(launchNames(launches, list)).toEqual({ started: ['n0', 'n2'], queued: ['n1'] })
    expect(launchNames(launches, undefined)).toEqual({ started: ['n0', 'n1', 'n2'], queued: [] })
  })
})

describe('launchProgress', () => {
  const launches = launchesOf(
    ['c1', 'c2', 'c3'].map((c, i) => ({ type: 'tool_call' as const, id: c, name: 'subagent', rawArguments: '{}', arguments: { name: `n${i}`, background: false } })),
    {},
  )
  const at = (minute: number, second = 0) => `2026-10-27T00:${String(minute).padStart(2, '0')}:${String(second).padStart(2, '0')}Z`
  const listed = (states: Subagent['state'][], finished: (string | undefined)[] = []) =>
    states.map((state, i) => agent({ id: id(`s${i}`), callId: `c${i + 1}`, state, createdAt: at(0, i), finishedAt: finished[i] }))

  it('names them while none has ended', () => {
    expect(launchProgress(launches, listed(['running', 'queued', 'running']))).toEqual({ kind: 'names', started: ['n0', 'n2'], queued: ['n1'] })
  })

  it('counts once one has ended', () => {
    expect(launchProgress(launches, listed(['completed', 'queued', 'running'], [at(1)]))).toEqual({
      kind: 'progress', total: 3, queued: 1, done: 1, stopped: 0, failed: 0,
    })
  })

  it('says how long all of it took once all have ended, and what did not finish', () => {
    expect(launchProgress(launches, listed(['completed', 'cancelled', 'completed'], [at(1, 7), at(2), at(3, 30)]))).toEqual({
      kind: 'finished', total: 3, durationMs: 210_000, stopped: 1, failed: 0, single: undefined,
    })
  })

  it('names a single one that ended', () => {
    const one = launches.slice(0, 1)
    expect(launchProgress(one, listed(['failed'], [at(0, 30)]))).toMatchObject({ kind: 'finished', total: 1, single: { name: 'n0', state: 'failed' } })
  })

  it('reads one not listed yet as its call says', () => {
    const call = { type: 'tool_call' as const, id: 'c', name: 'subagent', rawArguments: '{}', arguments: { name: 'x', background: false } }
    const one = launchesOf(
      [call],
      { c: { type: 'tool_result', callId: 'c', name: 'subagent', content: '', status: 'succeeded', details: { id: id('x'), name: 'x', background: false, state: 'completed', durationMs: 4000 } } },
    )
    expect(launchProgress(one, undefined)).toMatchObject({ kind: 'finished', durationMs: 4000 })
  })
})

describe('tally and the summary', () => {
  it('groups working and finished ones, newest first', () => {
    const list = [
      agent({ id: '1', state: 'running', createdAt: '2026-10-27T00:01:00Z', startedAt: '2026-10-27T00:01:00Z' }),
      agent({ id: '2', state: 'completed', finishedAt: '2026-10-27T00:02:00Z' }),
      agent({ id: '3', state: 'running', createdAt: '2026-10-27T00:03:00Z', startedAt: '2026-10-27T00:03:00Z' }),
      agent({ id: '4', state: 'cancelled', finishedAt: '2026-10-27T00:05:00Z' }),
    ]
    const { running, finished } = tally(list)
    expect(running.map((a) => a.id)).toEqual(['3', '1'])
    expect(finished.map((a) => a.id)).toEqual(['4', '2'])
  })

  it('keeps a queued one in the working group, in launch order, where it stays once it starts', () => {
    const queued = agent({ id: 'q', state: 'queued', createdAt: '2026-10-27T00:02:00Z', startedAt: '2026-10-27T00:02:00Z' })
    const list = [
      agent({ id: 'a', state: 'running', createdAt: '2026-10-27T00:01:00Z', startedAt: '2026-10-27T00:01:00Z' }),
      queued,
      agent({ id: 'b', state: 'running', createdAt: '2026-10-27T00:03:00Z', startedAt: '2026-10-27T00:03:00Z' }),
    ]
    const before = tally(list)
    expect(before.active.map((a) => a.id)).toEqual(['b', 'q', 'a'])
    expect([before.running.length, before.queued.length, before.finished.length]).toEqual([2, 1, 0])
    // It starts much later: same place.
    const after = tally(list.map((a) => (a.id === 'q' ? { ...queued, state: 'running' as const, startedAt: '2026-10-27T01:00:00Z' } : a)))
    expect(after.active.map((a) => a.id)).toEqual(['b', 'q', 'a'])
  })

  it('shows while one works, or since the panel was opened', () => {
    expect(showSummary([], true)).toBe(false)
    expect(showSummary([agent({ id: '1', state: 'completed' })], false)).toBe(false)
    expect(showSummary([agent({ id: '1', state: 'completed' })], true)).toBe(true)
    expect(showSummary([agent({ id: '1', state: 'running' })], false)).toBe(true)
    expect(showSummary([agent({ id: '1', state: 'queued' })], false)).toBe(true)
  })

  it('says how long ago', () => {
    const now = Date.parse('2026-10-27T12:00:00Z')
    expect(relativeAge('2026-10-27T11:59:30Z', now)).toEqual({ unit: 'now', count: 0 })
    expect(relativeAge('2026-10-27T11:57:00Z', now)).toEqual({ unit: 'minute', count: 3 })
    expect(relativeAge('2026-10-27T09:00:00Z', now)).toEqual({ unit: 'hour', count: 3 })
    expect(relativeAge('2026-10-25T12:00:00Z', now)).toEqual({ unit: 'day', count: 2 })
  })
})

describe('buildTurns with reports', () => {
  it('draws a report delivered while idle as a turn of its own, before the reply it started', () => {
    const r1 = id('r1')
    const r2 = id('r2')
    const rows = [
      user('q'),
      toolCallRow('call', r1, 'c1', 'subagent', { name: '调研', task: 't', background: true }),
      toolResultRow('result', r1, 'c1', 'subagent', { id: id('s'), name: '调研', background: true, state: 'running' }),
      assistant('launched', r1),
      reportRow('rep', report('调研', 's')),
      assistant('answer', r2),
    ]
    const turns = buildTurns(rows, null)
    expect(turns.map((t) => t.kind)).toEqual(['user', 'assistant', 'reports', 'assistant'])
    const reports = turns[2]
    expect(reports?.kind === 'reports' && reports.notices.map((n) => n.name)).toEqual(['调研'])
    // The report is no question: a regeneration redoes the person's question.
    expect(regenerable(turns)?.inputId).toBe(id('q'))
  })

  it('draws a report delivered into a run that was going inside its reply', () => {
    const r1 = id('r1')
    const rows = [
      user('q'),
      toolCallRow('call', r1, 'c1', 'subagent', { name: '调研', task: 't', background: true }),
      toolResultRow('result', r1, 'c1', 'subagent', { id: id('s'), name: '调研', background: true, state: 'running' }),
      reportRow('rep', report('调研', 's'), r1),
      assistant('answer', r1),
    ]
    const turns = buildTurns(rows, null)
    expect(turns.map((t) => t.kind)).toEqual(['user', 'assistant'])
    const reply = turns[1]
    if (reply?.kind !== 'assistant') throw new Error('no reply')
    expect(reply.responses).toHaveLength(2)
    expect(reply.reports).toHaveLength(1)
    expect(reply.reports[0]?.index).toBe(1)
  })

  it('keeps a report delivered at the first request of a live run in that run’s reply', () => {
    const r1 = id('r1')
    const live = liveRun(run('r1'), [user('q'), reportRow('rep', report('调研', 's'), r1)], {
      partial: [{ type: 'text', text: 'writing' }],
    })
    const turns = buildTurns([user('q')], live)
    expect(turns.map((t) => t.kind)).toEqual(['user', 'assistant'])
    const reply = turns[1]
    expect(reply?.kind === 'assistant' && [reply.key, reply.reports.length, reply.partial?.length]).toEqual([r1, 1, 1])
  })
})
