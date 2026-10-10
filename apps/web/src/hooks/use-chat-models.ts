import type { Conversation, UpdateConversationRequest } from '@openchat/api-client'
import { queryOptions, useMutation, useQuery, useQueryClient } from '@tanstack/react-query'

import { api, unwrap } from '@/lib/api'
import { queryKeys } from '@/lib/query-keys'

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
 * Remembers a conversation's model, reasoning effort and web access, so its
 * next run — and another device — use them. Applied to the cached conversation
 * at once: the picker shows the choice before the server has confirmed it.
 */
export function useUpdateConversationSettings() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async ({
      conversationId,
      ...body
    }: { conversationId: string } & Pick<UpdateConversationRequest, 'model' | 'reasoningEffort' | 'webSearch'>) =>
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
