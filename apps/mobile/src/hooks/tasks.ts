import type { CreateTaskRequest, Task, TaskList, UpdateTaskRequest } from '@openchat/api-client'
import { useMutation, useQuery, useQueryClient, type QueryClient } from '@tanstack/react-query'

import { queryKeys } from '@openchat/chat-core/query-keys'

import { api, unwrap } from '@/lib/api'

// Scheduled tasks, as the web's hooks/use-tasks.ts.

/** The person's tasks; asked again every minute, as their next runs move on. */
export function useTasks(enabled = true) {
  return useQuery({
    queryKey: queryKeys.tasks.list(),
    queryFn: async ({ signal }) => unwrap(await api.GET('/tasks', { signal })),
    enabled,
    refetchInterval: 60_000,
  })
}

/** Puts a task where the list reads it: replacing its old self, or first if new. */
function remember(queryClient: QueryClient, task: Task) {
  queryClient.setQueryData<TaskList>(queryKeys.tasks.list(), (held) =>
    held && {
      ...held,
      items: held.items.some((item) => item.id === task.id) ? held.items.map((item) => (item.id === task.id ? task : item)) : [task, ...held.items],
    },
  )
}

export function useCreateTask() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async (body: CreateTaskRequest) => unwrap(await api.POST('/tasks', { body })),
    onSuccess: (task) => {
      remember(queryClient, task)
      // Its conversation is new in the list.
      void queryClient.invalidateQueries({ queryKey: queryKeys.conversations.list() })
    },
  })
}

export function useUpdateTask() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async ({ id, body }: { id: string; body: UpdateTaskRequest }) =>
      unwrap(await api.PATCH('/tasks/{taskId}', { params: { path: { taskId: id } }, body })),
    onSuccess: (task, { body }) => {
      remember(queryClient, task)
      if (body.title !== undefined) void queryClient.invalidateQueries({ queryKey: queryKeys.conversations.list() })
    },
  })
}

export function useDeleteTask() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async (id: string) => unwrap(await api.DELETE('/tasks/{taskId}', { params: { path: { taskId: id } } })),
    onSuccess: (_data, id) => {
      queryClient.setQueryData<TaskList>(queryKeys.tasks.list(), (held) => held && { ...held, items: held.items.filter((item) => item.id !== id) })
    },
  })
}

/** Runs a task now, besides its schedule. */
export function useRunTask() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async (id: string) => unwrap(await api.POST('/tasks/{taskId}/run', { params: { path: { taskId: id } } })),
    onSuccess: (task) => {
      remember(queryClient, task)
      void queryClient.invalidateQueries({ queryKey: queryKeys.conversations.running() })
    },
  })
}
