import type { Conversation, ModelMark, UpdateConversationRequest } from '@openchat/api-client'
import { keepPreviousData, queryOptions, useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useCallback, useMemo } from 'react'

import { api, unwrap } from '@/lib/api'
import { missingModels } from '@openchat/chat-core/model-marks'
import { queryKeys } from '@openchat/chat-core/query-keys'

/** The models a conversation can use: whatever an enabled channel offers. */
export const chatModelsQueryOptions = queryOptions({
  queryKey: queryKeys.models.list(),
  queryFn: async ({ signal }) => unwrap(await api.GET('/models', { signal })),
  // Changes only when an administrator edits a channel.
  staleTime: 60_000,
})

export function useChatModels() {
  return useQuery(chatModelsQueryOptions)
}

/**
 * The name and mark of each model that wrote a reply: from the model list,
 * else - for a model since removed or turned off - from `GET /models/marks`,
 * so an old reply keeps its name and icon rather than showing a raw id.
 */
export function useModelMarks(ids: readonly (string | null | undefined)[]): (id: string | null | undefined) => ModelMark | undefined {
  const models = useChatModels()
  const listed = useMemo(() => (models.data ? new Map(models.data.map((m) => [m.id, m])) : undefined), [models.data])
  const key = ids.join('\n')
  // eslint-disable-next-line react-hooks/exhaustive-deps -- keyed on the ids' text, not the array's identity
  const missing = useMemo(() => missingModels(ids, listed ? new Set(listed.keys()) : undefined), [key, listed])
  const marks = useQuery({
    queryKey: queryKeys.models.marks(missing),
    queryFn: async ({ signal }) => unwrap(await api.GET('/models/marks', { params: { query: { ids: missing } }, signal })),
    enabled: missing.length > 0,
    // A removed model's name does not change; one more reply naming another
    // keeps the ones already known on screen while it is asked for.
    staleTime: Infinity,
    placeholderData: keepPreviousData,
  })
  return useCallback(
    (id: string | null | undefined) => (id ? (listed?.get(id) ?? marks.data?.find((m) => m.id === id)) : undefined),
    [listed, marks.data],
  )
}

/** What the composer can offer: web search, when an administrator has it on. */
export const chatFeaturesQueryOptions = queryOptions({
  queryKey: queryKeys.features,
  queryFn: async ({ signal }) => unwrap(await api.GET('/features', { signal })),
  staleTime: 60_000,
})

export function useChatFeatures() {
  return useQuery(chatFeaturesQueryOptions)
}

/**
 * Remembers a conversation's model, reasoning effort, web access and browser, so its
 * next run — and another device — use them. Applied to the cached conversation
 * at once: the picker shows the choice before the server has confirmed it.
 */
export function useUpdateConversationSettings() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async ({
      conversationId,
      ...body
    }: { conversationId: string } & Pick<UpdateConversationRequest, 'model' | 'reasoningEffort' | 'webSearch' | 'browser'>) =>
      unwrap(
        await api.PATCH('/conversations/{conversationId}', {
          params: { path: { conversationId } },
          body,
        }),
      ),
    onMutate: ({ conversationId, ...change }) => {
      const key = queryKeys.conversations.detail(conversationId)
      const previous = queryClient.getQueryData<Conversation>(key)
      if (previous) queryClient.setQueryData(key, { ...previous, ...change })
      return { previous }
    },
    onError: (_error, { conversationId }, context) => {
      if (context?.previous) {
        queryClient.setQueryData(queryKeys.conversations.detail(conversationId), context.previous)
      }
    },
    onSuccess: (conversation: Conversation) => {
      queryClient.setQueryData(queryKeys.conversations.detail(conversation.id), conversation)
    },
  })
}
