import type { Conversation, UpdateConversationRequest } from '@openchat/api-client'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'

import { queryKeys } from '@openchat/chat-core/query-keys'

import { api, unwrap } from '@/lib/api'

const modelsQuery = {
  queryKey: queryKeys.models.list(),
  queryFn: async ({ signal }: { signal: AbortSignal }) => unwrap(await api.GET('/models', { signal })),
  staleTime: 60_000,
}

/** The models a conversation can use: whatever an enabled channel offers. */
export function useChatModels() {
  return useQuery(modelsQuery)
}

/** The list asked again now: whether a model is at its cap may have changed. */
export const freshModels = (queryClient: ReturnType<typeof useQueryClient>) =>
  queryClient.fetchQuery({ ...modelsQuery, staleTime: 0 })

/** What the composer can offer: web search, when an administrator has it on. */
export function useChatFeatures() {
  return useQuery({
    queryKey: queryKeys.features,
    queryFn: async ({ signal }) => unwrap(await api.GET('/features', { signal })),
    staleTime: 60_000,
  })
}

/**
 * Remembers a conversation's model, reasoning effort and web access. Applied to
 * the cached conversation at once: the picker shows the choice before the
 * server has confirmed it.
 */
export function useUpdateConversationSettings() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async ({
      conversationId,
      ...body
    }: { conversationId: string } & Pick<UpdateConversationRequest, 'model' | 'reasoningEffort' | 'webSearch'>) =>
      unwrap(await api.PATCH('/conversations/{conversationId}', { params: { path: { conversationId } }, body })),
    onMutate: ({ conversationId, ...change }) => {
      const key = queryKeys.conversations.detail(conversationId)
      const previous = queryClient.getQueryData<Conversation>(key)
      if (previous) queryClient.setQueryData(key, { ...previous, ...change })
      return { previous }
    },
    onError: (_error, { conversationId }, context) => {
      if (context?.previous) queryClient.setQueryData(queryKeys.conversations.detail(conversationId), context.previous)
    },
    onSuccess: (conversation: Conversation) => {
      queryClient.setQueryData(queryKeys.conversations.detail(conversation.id), conversation)
    },
  })
}
