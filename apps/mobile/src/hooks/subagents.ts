import { isOpenChatError, type SubagentDetail, type SubagentList } from '@openchat/api-client'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'

import { queryKeys } from '@openchat/chat-core/query-keys'
import { durationParts } from '@openchat/chat-core/subagents'

import { api, unwrap } from '@/lib/api'
import { toast } from '@/lib/toast'

// A conversation's sub-agents, as the web's hooks/use-subagents.ts.

const LIST_POLL_MS = 3000
const DETAIL_POLL_MS = 1000

/** The conversation's sub-agents; asked again every few seconds while any is working. */
export function useSubagents(conversationId: string | undefined, enabled = true) {
  return useQuery({
    queryKey: queryKeys.conversations.subagents(conversationId ?? ''),
    queryFn: async ({ signal }): Promise<SubagentList> =>
      unwrap(await api.GET('/conversations/{conversationId}/subagents', { params: { path: { conversationId: conversationId! } }, signal })),
    enabled: enabled && Boolean(conversationId),
    refetchInterval: (query) => (query.state.data?.items.some((a) => a.state === 'running') ? LIST_POLL_MS : false),
  })
}

/** One sub-agent's work, kept current while it works. */
export function useSubagent(conversationId: string, subagentId: string | null) {
  return useQuery({
    queryKey: queryKeys.conversations.subagent(conversationId, subagentId ?? ''),
    queryFn: async ({ signal }): Promise<SubagentDetail> =>
      unwrap(
        await api.GET('/conversations/{conversationId}/subagents/{subagentId}', {
          params: { path: { conversationId, subagentId: subagentId! } },
          signal,
        }),
      ),
    enabled: Boolean(subagentId),
    refetchInterval: (query) => (query.state.data?.subagent.state === 'running' ? DETAIL_POLL_MS : false),
  })
}

/** Stop, continue, or let the reply waiting on it end (`background`). */
export function useSubagentAction(conversationId: string) {
  const queryClient = useQueryClient()
  const { t } = useTranslation()
  return useMutation({
    mutationFn: async ({ action, id }: { action: 'stop' | 'resume' | 'background'; id: string }) => {
      const params = { params: { path: { conversationId, subagentId: id } } }
      unwrap(
        action === 'stop'
          ? await api.POST('/conversations/{conversationId}/subagents/{subagentId}/stop', params)
          : action === 'resume'
            ? await api.POST('/conversations/{conversationId}/subagents/{subagentId}/resume', params)
            : await api.POST('/conversations/{conversationId}/subagents/{subagentId}/background', params),
      )
    },
    onSettled: () => {
      void queryClient.invalidateQueries({ queryKey: queryKeys.conversations.subagents(conversationId) })
      void queryClient.invalidateQueries({ queryKey: [...queryKeys.conversations.all, 'subagent', conversationId] })
    },
    onError: (error) => {
      const code = isOpenChatError(error) ? error.code : ''
      toast.error(code === 'too_many_subagents' ? t('subagents.error.too-many') : code === 'subagent_busy' ? t('subagents.error.busy') : t('subagents.error.generic'))
    },
  })
}

/** "3 分 12 秒". */
export function useDuration() {
  const { t } = useTranslation()
  return (ms: number) => {
    const { minutes, seconds } = durationParts(ms)
    return minutes > 0 ? t('subagents.duration.minutes', { minutes, seconds }) : t('subagents.duration.seconds', { seconds })
  }
}
