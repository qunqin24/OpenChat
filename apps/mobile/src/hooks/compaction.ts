import { isOpenChatError, type CompactionState, type ContextSummary } from '@openchat/api-client'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'

import { setCompaction } from '@openchat/chat-core/conversation-follow'
import { queryKeys } from '@openchat/chat-core/query-keys'

import { api, unwrap } from '@/lib/api'
import { toast } from '@/lib/toast'

/** The conversation's compressed context, kept current by the events stream. */
export function useCompaction(conversationId: string | undefined): ContextSummary | undefined {
  const query = useQuery({
    queryKey: queryKeys.conversations.compaction(conversationId ?? ''),
    queryFn: async ({ signal }): Promise<CompactionState> =>
      unwrap(await api.GET('/conversations/{conversationId}/compaction', { params: { path: { conversationId: conversationId! } }, signal })),
    enabled: Boolean(conversationId),
  })
  return query.data?.summary
}

/** Takes the compression back: the model is sent the whole conversation again. */
export function useUndoCompaction(conversationId: string) {
  const queryClient = useQueryClient()
  const { t } = useTranslation()
  return useMutation({
    meta: { silent: true },
    mutationFn: async () =>
      unwrap(await api.DELETE('/conversations/{conversationId}/compaction', { params: { path: { conversationId } } })),
    onSuccess: () => {
      setCompaction(queryClient, conversationId, undefined)
      void queryClient.invalidateQueries({ queryKey: queryKeys.conversations.context(conversationId) })
      toast.info(t('chat.compaction.undone'))
    },
    onError: (error) =>
      toast.error(
        isOpenChatError(error) && error.code === 'conversation_busy'
          ? t('chat.compaction.error-conversation_busy')
          : t('chat.compaction.error-generic'),
      ),
  })
}
