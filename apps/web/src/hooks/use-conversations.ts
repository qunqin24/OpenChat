import type { Conversation, CreateConversationRequest } from '@openchat/api-client'
import { infiniteQueryOptions, queryOptions, useInfiniteQuery, useMutation, useQuery, useQueryClient } from '@tanstack/react-query'

import { api, unwrap } from '@/lib/api'
import { queryKeys } from '@/lib/query-keys'

/** The conversation list, newest first, a page at a time. Shared with the route loader. */
export const conversationsQueryOptions = infiniteQueryOptions({
  queryKey: queryKeys.conversations.list(),
  initialPageParam: undefined as string | undefined,
  queryFn: async ({ signal, pageParam }) =>
    unwrap(
      await api.GET('/conversations', {
        params: { query: { limit: 50, cursor: pageParam } },
        signal,
      }),
    ),
  getNextPageParam: (page) => page.nextCursor ?? undefined,
})

export function useConversations() {
  return useInfiniteQuery({ ...conversationsQueryOptions, placeholderData: (previous) => previous })
}

/** One conversation's settings and title. Shared with the route loader. */
export const conversationQueryOptions = (conversationId: string) =>
  queryOptions({
    queryKey: queryKeys.conversations.detail(conversationId),
    queryFn: async ({ signal }) =>
      unwrap(
        await api.GET('/conversations/{conversationId}', {
          params: { path: { conversationId } },
          signal,
        }),
      ),
  })

export function useConversation(conversationId: string) {
  return useQuery({ ...conversationQueryOptions(conversationId), enabled: Boolean(conversationId) })
}

export function useCreateConversation() {
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: async (body: CreateConversationRequest) =>
      unwrap(await api.POST('/conversations', { body })),
    onSuccess: (conversation: Conversation) => {
      queryClient.setQueryData(queryKeys.conversations.detail(conversation.id), conversation)
      void queryClient.invalidateQueries({ queryKey: queryKeys.conversations.list() })
    },
  })
}

export function useRenameConversation() {
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: async ({ conversationId, title }: { conversationId: string; title: string }) =>
      unwrap(
        await api.PATCH('/conversations/{conversationId}', {
          params: { path: { conversationId } },
          body: { title },
        }),
      ),
    onSuccess: (conversation: Conversation) => {
      queryClient.setQueryData(queryKeys.conversations.detail(conversation.id), conversation)
      void queryClient.invalidateQueries({ queryKey: queryKeys.conversations.list() })
    },
  })
}

export function useDeleteConversation() {
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: async (conversationId: string) =>
      unwrap(
        await api.DELETE('/conversations/{conversationId}', {
          params: { path: { conversationId } },
        }),
      ),
    onSuccess: (_data, conversationId) => {
      queryClient.removeQueries({ queryKey: queryKeys.conversations.detail(conversationId) })
      queryClient.removeQueries({ queryKey: queryKeys.messages.list(conversationId) })
      void queryClient.invalidateQueries({ queryKey: queryKeys.conversations.list() })
    },
  })
}
