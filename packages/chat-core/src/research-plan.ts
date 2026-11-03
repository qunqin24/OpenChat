import type { ToolCallPart, ToolResultPart } from '@openchat/api-client'

/**
 * A deep research's plan (docs/design/deep-research-plan.md): the model puts
 * it with `research_plan` and the run stops there, as with ask_user; the
 * person starts it, asks for changes, or cancels it.
 */
export const RESEARCH_PLAN = 'research_plan'

/** How long the plan card counts down before it starts the research by itself. */
export const PLAN_COUNTDOWN_SECONDS = 60

export interface ResearchPlan {
  title: string
  steps: string[]
}

export type PlanDecision = 'started' | 'revised' | 'cancelled'

/** A call's plan, or null while its arguments are still streaming. */
export function parsePlan(args: ToolCallPart['arguments']): ResearchPlan | null {
  const title = args?.title
  const steps = args?.steps
  if (typeof title !== 'string' || !title.trim() || !Array.isArray(steps)) return null
  const lines = steps.filter((s): s is string => typeof s === 'string' && s.trim() !== '').map((s) => s.trim())
  return lines.length > 0 ? { title: title.trim(), steps: lines } : null
}

/** What the person did with a plan, from the call's result. */
export function planDecisionOf(result: ToolResultPart | undefined): { decision: PlanDecision; feedback?: string } | null {
  if (result?.status !== 'succeeded') return null
  const decision = result.details?.decision
  if (decision !== 'started' && decision !== 'revised' && decision !== 'cancelled') return null
  const feedback = result.details?.feedback
  return { decision, ...(typeof feedback === 'string' && feedback ? { feedback } : {}) }
}

/**
 * Seconds left of a plan's countdown, from when the reply that put it was
 * written; 0 once it is over. A plan come back to after the countdown does not
 * start by itself: it waits for the person.
 */
export function planSecondsLeft(writtenAt: string, now: number): number {
  const at = Date.parse(writtenAt)
  if (Number.isNaN(at)) return 0
  return Math.max(0, Math.ceil((at + PLAN_COUNTDOWN_SECONDS * 1000 - now) / 1000))
}
