import type { MemoryEvent, UpdatePersonalizationRequest } from '@openchat/api-client'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'

import { api, unwrap } from '@/lib/api'
import { queryKeys } from '@openchat/chat-core/query-keys'

/**
 * A person's own memory: custom instructions, the notes, and the log of what
 * the model changed. None of it is persisted to the browser's storage
 * (lib/query-persist.ts keeps only an allow-list), and every call is to /me.
 */

export const personalizationQueryOptions = {
  queryKey: queryKeys.memory.personalization(),
  queryFn: async ({ signal }: { signal: AbortSignal }) => unwrap(await api.GET('/me/personalization', { signal })),
}

export function usePersonalization() {
  return useQuery(personalizationQueryOptions)
}

export function useUpdatePersonalization() {
  const queryClient = useQueryClient()
  return useMutation({
    // Each control reports its own outcome.
    meta: { silent: true },
    mutationFn: async (body: UpdatePersonalizationRequest) => unwrap(await api.PATCH('/me/personalization', { body })),
    onSuccess: (next) => queryClient.setQueryData(queryKeys.memory.personalization(), next),
  })
}

export function useMemories() {
  return useQuery({
    queryKey: queryKeys.memory.list(),
    queryFn: async ({ signal }) => unwrap(await api.GET('/me/memories', { signal })).items,
  })
}

/** Everything a change to the list makes stale: the list, and the log beside it. */
function useRefreshMemory() {
  const queryClient = useQueryClient()
  return () => queryClient.invalidateQueries({ queryKey: queryKeys.memory.all })
}

export function useCreateMemory() {
  const refresh = useRefreshMemory()
  return useMutation({
    meta: { silent: true },
    mutationFn: async (content: string) => unwrap(await api.POST('/me/memories', { body: { content } })),
    onSuccess: refresh,
  })
}

export function useUpdateMemory() {
  const refresh = useRefreshMemory()
  return useMutation({
    meta: { silent: true },
    mutationFn: async ({ id, content }: { id: string; content: string }) =>
      unwrap(await api.PATCH('/me/memories/{memoryId}', { params: { path: { memoryId: id } }, body: { content } })),
    onSuccess: refresh,
  })
}

export function useDeleteMemory() {
  const refresh = useRefreshMemory()
  return useMutation({
    meta: { silent: true },
    mutationFn: async (id: string) =>
      unwrap(await api.DELETE('/me/memories/{memoryId}', { params: { path: { memoryId: id } } })),
    onSuccess: refresh,
  })
}

export function useClearMemories() {
  const refresh = useRefreshMemory()
  return useMutation({
    meta: { silent: true },
    mutationFn: async () => unwrap(await api.DELETE('/me/memories')),
    onSuccess: refresh,
  })
}

/** What the model changed in the last thirty days, newest first. */
export function useMemoryEvents() {
  return useQuery({
    queryKey: queryKeys.memory.events(),
    queryFn: async ({ signal }) => unwrap(await api.GET('/me/memory-events', { signal })).items,
  })
}

export function useUndoMemoryEvent() {
  const refresh = useRefreshMemory()
  return useMutation({
    meta: { silent: true },
    mutationFn: async (event: MemoryEvent | string) =>
      unwrap(
        await api.POST('/me/memory-events/{eventId}/undo', {
          params: { path: { eventId: typeof event === 'string' ? event : event.id } },
        }),
      ),
    onSettled: refresh,
  })
}
