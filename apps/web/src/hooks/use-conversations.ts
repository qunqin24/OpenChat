import type { Conversation, CreateConversationRequest } from '@openchat/api-client'
import { infiniteQueryOptions, type QueryClient, queryOptions, useInfiniteQuery, useMutation, useQuery, useQueryClient } from '@tanstack/react-query'

import { useEffect, useMemo, useRef } from 'react'

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

/** The list and any search over it: both show titles and which conversations exist. */
function refreshLists(queryClient: QueryClient) {
  void queryClient.invalidateQueries({ queryKey: queryKeys.conversations.list() })
  void queryClient.invalidateQueries({ queryKey: [...queryKeys.conversations.all, 'search'] })
}

/**
 * Conversations whose title or anything said in them contains `term`, newest
 * first, a page at a time; each found by its words carries the passage. Off
 * while the term is empty.
 */
export function useConversationSearch(term: string) {
  return useInfiniteQuery({
    queryKey: queryKeys.conversations.search(term),
    initialPageParam: undefined as string | undefined,
    queryFn: async ({ signal, pageParam }) =>
      unwrap(
        await api.GET('/conversations', {
          params: { query: { limit: 30, cursor: pageParam, search: term } },
          signal,
        }),
      ),
    getNextPageParam: (page) => page.nextCursor ?? undefined,
    enabled: term !== '',
    // The previous term's results stay up while the next one's load, so the
    // list does not flash empty on every keystroke.
    placeholderData: (previous) => previous,
    gcTime: 60_000,
  })
}

/**
 * The conversations being answered right now, for the sidebar. Asked again
 * every few seconds while any is, and not otherwise: a run is started from a
 * conversation's own page, whose event stream invalidates this the moment it
 * starts. When one finishes, the list is refetched too — a conversation that
 * finished out of sight may have moved, or been given its title.
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
      refreshLists(queryClient)
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
      refreshLists(queryClient)
    },
  })
}

/**
 * Names a conversation from what it is about: the server has the task model
 * read its latest exchanges and renames it to the result. Takes as long as
 * the model does.
 */
export function useGenerateTitle() {
  const queryClient = useQueryClient()

  return useMutation({
    // The row shows it is being named.
    meta: { silent: true },
    mutationFn: async (conversationId: string) =>
      unwrap(
        await api.POST('/conversations/{conversationId}/title', {
          params: { path: { conversationId } },
        }),
      ),
    onSuccess: (conversation: Conversation) => {
      queryClient.setQueryData(queryKeys.conversations.detail(conversation.id), conversation)
      refreshLists(queryClient)
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
      refreshLists(queryClient)
    },
  })
}
