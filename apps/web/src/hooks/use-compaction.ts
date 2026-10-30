import { isOpenChatError, type CompactionState, type ContextSummary } from '@openchat/api-client'
import { setCompaction } from '@openchat/chat-core/conversation-follow'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { api, unwrap } from '@/lib/api'
import { queryKeys } from '@openchat/chat-core/query-keys'

/**
 * The conversation's compressed context: the summary that stands in, for the
 * model, for the messages up to `throughMessageId`. The messages stay in the
 * transcript; the page draws a divider after that one and the summary behind it.
 * Made only by runs; kept in the cache by the events stream (`compaction_finished`,
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


/**
 * Codes taking a compression back can be refused with that have their own
 * wording.
 */
const UNDO_ERRORS = ['conversation_busy'] as const

function useUndoError() {
  const { t } = useTranslation()
  return (error: Error) => {
    const code = isOpenChatError(error) ? error.code : ''
    const known = UNDO_ERRORS.find((c) => c === code)
    toast.error(known ? t(`chat.compaction.error-${known}`) : t('chat.compaction.error-generic'))
  }
}

/** Takes the compression back: the model is sent the whole conversation again. */
export function useUndoCompaction(conversationId: string) {
  const queryClient = useQueryClient()
  const { t } = useTranslation()
  const onError = useUndoError()
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
