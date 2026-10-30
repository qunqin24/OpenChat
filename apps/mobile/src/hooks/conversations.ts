import type { Conversation, CreateConversationRequest } from '@openchat/api-client'
import { useInfiniteQuery, useMutation, useQuery, useQueryClient, type QueryClient } from '@tanstack/react-query'
import { useEffect, useMemo, useRef } from 'react'

import { queryKeys } from '@openchat/chat-core/query-keys'
import { emptyTranscript, type TranscriptPages } from '@openchat/chat-core/transcript-pages'

import { api, unwrap } from '@/lib/api'

// The conversation list and one conversation's settings, as the web's
// hooks/use-conversations.ts has them (same keys, same cache moves).

/** The list and any search over it: both show titles and which conversations exist. */
function refreshLists(queryClient: QueryClient) {
  void queryClient.invalidateQueries({ queryKey: queryKeys.conversations.list() })
  void queryClient.invalidateQueries({ queryKey: [...queryKeys.conversations.all, 'search'] })
}

/** The conversation list, newest first, a page at a time. */
export function useConversations() {
  return useInfiniteQuery({
    queryKey: queryKeys.conversations.list(),
    initialPageParam: undefined as string | undefined,
    queryFn: async ({ signal, pageParam }) =>
      unwrap(await api.GET('/conversations', { params: { query: { limit: 50, cursor: pageParam } }, signal })),
    getNextPageParam: (page) => page.nextCursor ?? undefined,
    placeholderData: (previous) => previous,
  })
}

/** Conversations whose title or anything said in them contains `term`; off while it is empty. */
export function useConversationSearch(term: string) {
  return useInfiniteQuery({
    queryKey: queryKeys.conversations.search(term),
    initialPageParam: undefined as string | undefined,
    queryFn: async ({ signal, pageParam }) =>
      unwrap(await api.GET('/conversations', { params: { query: { limit: 30, cursor: pageParam, search: term } }, signal })),
    getNextPageParam: (page) => page.nextCursor ?? undefined,
    enabled: term !== '',
    placeholderData: (previous) => previous,
    gcTime: 60_000,
  })
}

/** The pinned conversations, the latest pinned first: shown above the list whatever their age. */
export function usePinnedConversations() {
  return useQuery({
    queryKey: queryKeys.conversations.pinned(),
    queryFn: async ({ signal }) => unwrap(await api.GET('/conversations', { params: { query: { pinned: true } }, signal })).items,
  })
}

/**
 * The conversations being answered right now. Asked again every few seconds
 * while any is; when one finishes, the list is refetched too (it may have
 * moved, or been given its title).
 */
export function useRunningConversations() {
  const queryClient = useQueryClient()
  const query = useQuery({
    queryKey: queryKeys.conversations.running(),
    queryFn: async ({ signal }) => unwrap(await api.GET('/conversations/running', { signal })).ids,
    refetchInterval: (q) => ((q.state.data?.length ?? 0) > 0 ? 3000 : false),
  })
  const ids = query.data
  const previous = useRef<string[]>([])
  useEffect(() => {
    if (!ids) return
    if (previous.current.some((id) => !ids.includes(id))) {
      void queryClient.invalidateQueries({ queryKey: queryKeys.conversations.list() })
    }
    previous.current = ids
  }, [ids, queryClient])
  return useMemo(() => new Set(ids ?? []), [ids])
}

export function useConversation(conversationId: string | undefined) {
  return useQuery({
    queryKey: queryKeys.conversations.detail(conversationId ?? ''),
    queryFn: async ({ signal }) =>
      unwrap(await api.GET('/conversations/{conversationId}', { params: { path: { conversationId: conversationId! } }, signal })),
    enabled: Boolean(conversationId),
  })
}

export function useCreateConversation() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async (body: CreateConversationRequest) => unwrap(await api.POST('/conversations', { body })),
    onSuccess: (conversation: Conversation) => {
      queryClient.setQueryData(queryKeys.conversations.detail(conversation.id), conversation)
      queryClient.setQueryData<TranscriptPages>(queryKeys.messages.list(conversation.id), emptyTranscript())
      refreshLists(queryClient)
    },
  })
}

/** Changes a conversation's title or pin, and puts the result where the lists read it. */
export function useUpdateConversation() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async ({ conversationId, ...body }: { conversationId: string; title?: string; pinned?: boolean }) =>
      unwrap(await api.PATCH('/conversations/{conversationId}', { params: { path: { conversationId } }, body })),
    onSuccess: (conversation: Conversation, { pinned }) => {
      queryClient.setQueryData(queryKeys.conversations.detail(conversation.id), conversation)
      if (pinned !== undefined) {
        queryClient.setQueryData<Conversation[]>(queryKeys.conversations.pinned(), (held) => {
          const others = (held ?? []).filter((item) => item.id !== conversation.id)
          return conversation.pinnedAt ? [conversation, ...others] : others
        })
      }
      refreshLists(queryClient)
    },
  })
}

export function useDeleteConversation() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async (conversationId: string) =>
      unwrap(await api.DELETE('/conversations/{conversationId}', { params: { path: { conversationId } } })),
    onSuccess: (_data, conversationId) => {
      queryClient.removeQueries({ queryKey: queryKeys.conversations.detail(conversationId) })
      queryClient.removeQueries({ queryKey: queryKeys.messages.list(conversationId) })
      void queryClient.invalidateQueries({ queryKey: queryKeys.conversations.pinned() })
      refreshLists(queryClient)
    },
  })
}
