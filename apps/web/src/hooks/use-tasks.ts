import type { CreateTaskRequest, Task, TaskList, UpdateTaskRequest } from '@openchat/api-client'
import { useMutation, useQuery, useQueryClient, type QueryClient } from '@tanstack/react-query'

import { api, unwrap } from '@/lib/api'
import { queryKeys } from '@openchat/chat-core/query-keys'

/** The person's scheduled tasks. Asked again now and then: the scheduler moves them on. */
export function useTasks(enabled = true) {
  return useQuery({
    queryKey: queryKeys.tasks.list(),
    queryFn: async ({ signal }) => unwrap(await api.GET('/tasks', { signal })),
    enabled,
    refetchInterval: 60_000,
  })
}

/** A task as the server now has it, in the cached list. */
function remember(queryClient: QueryClient, task: Task) {
  queryClient.setQueryData<TaskList>(queryKeys.tasks.list(), (held) =>
    held && { ...held, items: held.items.some((item) => item.id === task.id)
      ? held.items.map((item) => (item.id === task.id ? task : item))
      : [task, ...held.items] },
  )
}

export function useCreateTask() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async (body: CreateTaskRequest) => unwrap(await api.POST('/tasks', { body })),
    onSuccess: (task) => {
      remember(queryClient, task)
      // It made a conversation, named after it.
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
