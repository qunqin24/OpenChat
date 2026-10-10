import { isOpenChatError, type CompactionState, type ContextSummary } from '@openchat/api-client'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { api, unwrap } from '@/lib/api'
import { queryKeys } from '@/lib/query-keys'

/** Codes a compression can be refused with that have their own wording. */
const COMPACTION_ERRORS = ['nothing_to_compact', 'conversation_busy', 'compaction_busy', 'compaction_failed'] as const

/**
 * The conversation's compressed context: the summary that stands in, for the
 * model, for the messages up to `throughMessageId`. The messages stay in the
 * transcript; the page draws a divider after that one and the summary behind it.
 * Kept in the cache by the events stream (`compaction_finished`,
 * `compaction_changed`) and read again when rows were deleted, which can delete
 * the summary with them.
 */
export function useCompaction(conversationId: string | undefined): ContextSummary | undefined {
  const query = useQuery({
    queryKey: queryKeys.conversations.compaction(conversationId ?? ''),
    queryFn: async ({ signal }): Promise<CompactionState> =>
      unwrap(
        await api.GET('/conversations/{conversationId}/compaction', {
          params: { path: { conversationId: conversationId ?? '' } },
          signal,
        }),
      ),
    enabled: Boolean(conversationId),
  })
  return query.data?.summary
}

/** Puts a summary — or none — where the transcript reads it. */
export function setCompaction(
  queryClient: ReturnType<typeof useQueryClient>,
  conversationId: string,
  summary: ContextSummary | undefined,
) {
  queryClient.setQueryData<CompactionState>(queryKeys.conversations.compaction(conversationId), { summary })
}

function useCompactionError() {
  const { t } = useTranslation()
  return (error: Error) => {
    const code = isOpenChatError(error) ? error.code : ''
    const known = COMPACTION_ERRORS.find((c) => c === code)
    toast.error(known ? t(`chat.compaction.error-${known}`) : t('chat.compaction.error-generic'))
  }
}

/**
 * Compresses the whole conversation now, on the person's request, with the
 * conversation's own model. The answer to an unanswered question stays out of
 * it (the server keeps the question), which is what lets "compress, then answer
 * again" work after an answer failed for its length.
 */
export function useCompact(conversationId: string) {
  const queryClient = useQueryClient()
  const onError = useCompactionError()
  return useMutation({
    meta: { silent: true },
    mutationFn: async () =>
      unwrap(
        await api.POST('/conversations/{conversationId}/compaction', { params: { path: { conversationId } } }),
      ),
    onSuccess: (state) => {
      setCompaction(queryClient, conversationId, state.summary)
      // The meter reads the last request; the next one will be smaller.
      void queryClient.invalidateQueries({ queryKey: queryKeys.conversations.context(conversationId) })
    },
    onError,
  })
}

/** Takes the compression back: the model is sent the whole conversation again. */
export function useUndoCompaction(conversationId: string) {
  const queryClient = useQueryClient()
  const { t } = useTranslation()
  const onError = useCompactionError()
  return useMutation({
    meta: { silent: true },
    mutationFn: async () =>
      unwrap(
        await api.DELETE('/conversations/{conversationId}/compaction', { params: { path: { conversationId } } }),
      ),
    onSuccess: () => {
      setCompaction(queryClient, conversationId, undefined)
      void queryClient.invalidateQueries({ queryKey: queryKeys.conversations.context(conversationId) })
      toast.success(t('chat.compaction.undone'))
    },
    onError,
  })
}
