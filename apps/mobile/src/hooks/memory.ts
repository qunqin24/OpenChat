import type { MemoryEvent, UpdatePersonalizationRequest } from '@openchat/api-client'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'

import { memoryErrorText, saveRefusalText, undoErrorText } from '@openchat/chat-core/memory-errors'
import { queryKeys } from '@openchat/chat-core/query-keys'

import { api, unwrap } from '@/lib/api'

/**
 * A person's own memory (the web's use-memory.ts): custom instructions, the
 * notes, and the log of what the model changed. Every call is to /me, and none
 * of it is kept on the device past the app's life.
 */

export function usePersonalization() {
  return useQuery({
    queryKey: queryKeys.memory.personalization(),
    queryFn: async ({ signal }) => unwrap(await api.GET('/me/personalization', { signal })),
  })
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
    mutationFn: async (id: string) => unwrap(await api.DELETE('/me/memories/{memoryId}', { params: { path: { memoryId: id } } })),
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
    mutationFn: async (event: MemoryEvent) =>
      unwrap(await api.POST('/me/memory-events/{eventId}/undo', { params: { path: { eventId: event.id } } })),
    onSettled: refresh,
  })
}

// The wording is chat-core's (memory-errors.ts), shared with the web; these
// only supply the reader's language.

/** The words for a save that content moderation turned away, or null when that is not why it failed. */
export function useSaveRefusalText() {
  const { t } = useTranslation()
  return (error: unknown) => saveRefusalText(t, error)
}

/** The words for a failed write to the memory list. */
export function useMemoryErrorText() {
  const { t } = useTranslation()
  return (error: unknown) => memoryErrorText(t, error)
}

/** The words for an undo that could not be made. */
export function useUndoErrorText() {
  const { t } = useTranslation()
  return (error: unknown) => undoErrorText(t, error)
}
