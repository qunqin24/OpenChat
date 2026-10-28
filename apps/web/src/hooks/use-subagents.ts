import { isOpenChatError, type Subagent, type SubagentDetail, type SubagentList } from '@openchat/api-client'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { api, unwrap } from '@/lib/api'
import { queryKeys } from '@/lib/query-keys'
import { durationParts } from '@/lib/subagents'

/** How often the list is read again while a sub-agent works: its step count and activity move. */
const LIST_POLL_MS = 3000
/** How often a working sub-agent's own view is read again. */
const DETAIL_POLL_MS = 1000

/**
 * A conversation's sub-agents. Kept current by the events stream
 * (`subagents_changed`, and a delivered report), and read again every few
 * seconds while one works. Never persisted: it quotes what the model wrote.
 */
export function useSubagents(conversationId: string | undefined, enabled: boolean) {
  return useQuery({
    queryKey: queryKeys.conversations.subagents(conversationId ?? ''),
    queryFn: async ({ signal }): Promise<SubagentList> =>
      unwrap(
        await api.GET('/conversations/{conversationId}/subagents', {
          params: { path: { conversationId: conversationId ?? '' } },
          signal,
        }),
      ),
    enabled: enabled && Boolean(conversationId),
    refetchInterval: (query) => (query.state.data?.items.some((a) => a.state === 'running') ? LIST_POLL_MS : false),
  })
}

/** One sub-agent's work, read-only, read again every second while it works. */
export function useSubagent(conversationId: string, subagentId: string | null) {
  return useQuery({
    queryKey: queryKeys.conversations.subagent(conversationId, subagentId ?? ''),
    queryFn: async ({ signal }): Promise<SubagentDetail> =>
      unwrap(
        await api.GET('/conversations/{conversationId}/subagents/{subagentId}', {
          params: { path: { conversationId, subagentId: subagentId ?? '' } },
          signal,
        }),
      ),
    enabled: Boolean(subagentId),
    refetchInterval: (query) => (query.state.data?.subagent.state === 'running' ? DETAIL_POLL_MS : false),
  })
}

type Action = 'stop' | 'resume' | 'background'

/** Stopping a sub-agent, letting it go on, moving it to the background. */
export function useSubagentAction(conversationId: string) {
  const queryClient = useQueryClient()
  const { t } = useTranslation()
  return useMutation({
    mutationFn: async ({ action, id }: { action: Action; id: string }) => {
      const params = { params: { path: { conversationId, subagentId: id } } }
      const response =
        action === 'stop'
          ? await api.POST('/conversations/{conversationId}/subagents/{subagentId}/stop', params)
          : action === 'resume'
            ? await api.POST('/conversations/{conversationId}/subagents/{subagentId}/resume', params)
            : await api.POST('/conversations/{conversationId}/subagents/{subagentId}/background', params)
      unwrap(response)
    },
    onSettled: () => {
      void queryClient.invalidateQueries({ queryKey: queryKeys.conversations.subagents(conversationId) })
      void queryClient.invalidateQueries({ queryKey: [...queryKeys.conversations.all, 'subagent', conversationId] })
    },
    onError: (error) => {
      const code = isOpenChatError(error) ? error.code : ''
      toast.error(
        code === 'too_many_subagents'
          ? t('subagents.error.too-many')
          : code === 'subagent_busy'
            ? t('subagents.error.busy')
            : t('subagents.error.generic'),
      )
    },
  })
}

/** A sub-agent by id from the conversation's list, if it is there yet. */
export function subagentById(list: Subagent[] | undefined, id: string): Subagent | undefined {
  return list?.find((a) => a.id === id)
}

/** How long, as the lines say it: "3 分 12 秒" / "3m 12s". */
export function useDuration() {
  const { t } = useTranslation()
  return (ms: number) => {
    const { minutes, seconds } = durationParts(ms)
    return minutes > 0 ? t('subagents.duration.minutes', { minutes, seconds }) : t('subagents.duration.seconds', { seconds })
  }
}
