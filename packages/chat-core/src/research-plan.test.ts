import { describe, expect, it } from 'vitest'

import { parsePlan, planDecisionOf, planSecondsLeft } from './research-plan'

describe('research plan', () => {
  it('reads a plan, or nothing while it streams', () => {
    expect(parsePlan({ title: ' X ', steps: ['a', ' ', 'b '] })).toEqual({ title: 'X', steps: ['a', 'b'] })
    expect(parsePlan({ title: 'X' })).toBeNull()
    expect(parsePlan(undefined)).toBeNull()
  })

  it('reads what became of it', () => {
    expect(planDecisionOf({ type: 'tool_result', callId: 'c', name: 'research_plan', content: '', status: 'succeeded', details: { decision: 'revised', feedback: 'add Z' } })).toEqual({
      decision: 'revised',
      feedback: 'add Z',
    })
    expect(planDecisionOf(undefined)).toBeNull()
  })

  it('counts a minute down from when it was written', () => {
    const at = '2026-11-03T00:00:00Z'
    const t0 = Date.parse(at)
    expect(planSecondsLeft(at, t0)).toBe(60)
    expect(planSecondsLeft(at, t0 + 59_500)).toBe(1)
    expect(planSecondsLeft(at, t0 + 61_000)).toBe(0)
    expect(planSecondsLeft('', t0)).toBe(0)
  })
})
