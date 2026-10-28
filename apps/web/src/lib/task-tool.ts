import type { Task, ToolResultPart } from '@openchat/api-client'

/** The tool models schedule tasks with (api/task_tool.go). */
export const TASK_TOOL = 'scheduled_tasks'

/** What a succeeded call did (the server's TaskToolDetails); a list changes nothing. */
export interface TaskChange {
  op: 'create' | 'update' | 'pause' | 'resume' | 'delete'
  task?: Task
}

export function taskChangeOf(result: ToolResultPart | undefined): TaskChange | null {
  if (result?.status !== 'succeeded') return null
  const d = result.details
  if (!d || typeof d !== 'object') return null
  const op = d.op
  if (op !== 'create' && op !== 'update' && op !== 'pause' && op !== 'resume' && op !== 'delete') return null
  const task = d.task && typeof d.task === 'object' && typeof (d.task as Task).id === 'string' ? (d.task as Task) : undefined
  return { op, ...(task ? { task } : {}) }
}
