import { createFileRoute } from '@tanstack/react-router'

import TasksPage from '@/components/chat/TasksPage'

/** Scheduled tasks, beside the chat in the same shell. */
export const Route = createFileRoute('/_authed/tasks')({
  component: TasksPage,
})
