import { Link } from '@tanstack/react-router'
import { AlarmClock, ChevronRight } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import type { TaskChange } from '@/lib/task-tool'
import { describeSchedule } from '@/lib/tasks'

/**
 * What the model just did to the person's scheduled tasks, in one line of its
 * reply - "task scheduled", its name and when - with the way to the tasks page,
 * where it can be changed. Like the memory chip, not a step of the activity line.
 */
export function TaskChip({ change }: { change: TaskChange }) {
  const { t, i18n } = useTranslation()
  const task = change.task
  return (
    <Link className="n-memory-chip n-task-chip" to="/tasks" role="status">
      <AlarmClock className="n-memory-chip-icon" aria-hidden="true" />
      <span className="n-memory-chip-label">{t(`tasks.chip.${change.op}`)}</span>
      {task && (
        <span className="n-task-chip-text" dir="auto">
          {task.title} · {describeSchedule(task.schedule, t, i18n.resolvedLanguage)}
        </span>
      )}
      <ChevronRight className="n-task-chip-go" aria-hidden="true" />
    </Link>
  )
}

export default TaskChip
