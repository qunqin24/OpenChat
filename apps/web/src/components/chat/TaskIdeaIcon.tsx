import type { ReactNode } from 'react'

import type { TaskIdeaKey } from '@/lib/tasks'

/**
 * The drawings beside the scheduled-task ideas (TasksPage), made for them in the
 * starters' style (`.n-drawing`): line art on a 24 grid with one finishing
 * touch — the headline, the day that is circled, the moon over the bulb, the
 * leaf, the price going down, the coin — which the stylesheet colours by idea.
 */
const DRAWINGS: Record<TaskIdeaKey, ReactNode> = {
  // A newspaper folded open, its headline coloured.
  news: (
    <>
      <path d="M4 6.5a1.5 1.5 0 0 1 1.5-1.5h10a1.5 1.5 0 0 1 1.5 1.5V18a1.5 1.5 0 0 0 3 0V9.5h-3" />
      <path d="M18.5 19.5h-13A1.5 1.5 0 0 1 4 18V6.5" />
      <path className="is-accent" d="M7 8.8h6.5" />
      <path d="M7 12.3h6.5M7 15.5h4" />
    </>
  ),
  // A calendar with Friday marked.
  'weekly-review': (
    <>
      <rect x="4" y="5.5" width="16" height="14" rx="2.5" />
      <path d="M8 3.5v4M16 3.5v4M4 10h16" />
      <path d="M8 13.5h.01M11.3 13.5h.01M8 16.5h.01M11.3 16.5h.01" strokeWidth="2" />
      <rect className="is-accent-fill" x="14" y="12.4" width="3.2" height="3.2" rx="0.8" />
    </>
  ),
  // A light bulb under the moon: a little learning before bed.
  learn: (
    <>
      <path d="M8 16.2a5.2 5.2 0 1 1 6.4 0c-.5.4-.8 1-.8 1.6v.7H8.8v-.7c0-.6-.3-1.2-.8-1.6Z" />
      <path d="M9 21h4.4" />
      <path className="is-accent-fill" d="M19.6 3.2a2.6 2.6 0 1 0 1.9 4 2.1 2.1 0 0 1-1.9-4Z" />
    </>
  ),
  // A bowl with a leaf over it.
  meals: (
    <>
      <path d="M3.5 12.5h17c0 4-3.8 7-8.5 7s-8.5-3-8.5-7Z" />
      <path d="M9 19v1.5h6V19" />
      <path d="M9.5 12.5c-.3-1.6.3-2.9 1.6-3.6" />
      <path className="is-accent-fill" d="M13 12.2c-.1-3.6 1.9-6 5.6-6.4.1 3.7-1.9 6.1-5.6 6.4Z" />
    </>
  ),
  // A paper plane, and the price going down.
  deals: (
    <>
      <path d="M20.5 3.8 3.8 10.4l6.3 2.6 2.6 6.3Z" />
      <path d="m10.1 13 10.4-9.2" />
      <path className="is-accent" d="M5.5 14.5v5.5M3.6 18.1l1.9 1.9 1.9-1.9" />
    </>
  ),
  // A wallet with a coin at its clasp.
  monthly: (
    <>
      <path d="M4.5 8V7a2 2 0 0 1 2-2h10v3" />
      <rect x="4" y="8" width="16" height="11.5" rx="2.5" />
      <path d="M20 11.5h-3.3a2.25 2.25 0 0 0 0 4.5H20" />
      <circle className="is-accent-fill" cx="16.8" cy="13.75" r="1" />
    </>
  ),
}

export function TaskIdeaIcon({ idea }: { idea: TaskIdeaKey }) {
  return (
    <svg className="n-drawing n-tasks-idea-icon" viewBox="0 0 24 24" aria-hidden="true">
      {DRAWINGS[idea]}
    </svg>
  )
}

export default TaskIdeaIcon
