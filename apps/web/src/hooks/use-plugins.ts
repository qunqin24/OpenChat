import type { Plugin, PluginList } from '@openchat/api-client'
import { useMutation, useQuery, useQueryClient, type QueryClient } from '@tanstack/react-query'

import { api, unwrap } from '@/lib/api'
import { queryKeys } from '@openchat/chat-core/query-keys'

/** The plugins the person may install, featured first (api/plugins.go). */
export function usePlugins() {
  return useQuery({
    queryKey: queryKeys.plugins.list(),
    queryFn: async ({ signal }) => unwrap(await api.GET('/plugins', { signal })),
  })
}

/** Whether a plugin is installed, in the cached list. */
function remember(queryClient: QueryClient, id: string, installed: boolean) {
  queryClient.setQueryData<PluginList>(queryKeys.plugins.list(), (held) =>
    held && { ...held, items: held.items.map((item) => (item.id === id ? { ...item, installed } : item)) },
  )
}

export function useInstallPlugin() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async (id: string): Promise<Plugin> =>
      unwrap(await api.POST('/plugins/{pluginId}/install', { params: { path: { pluginId: id } } })),
    onSuccess: (plugin) => remember(queryClient, plugin.id, true),
  })
}

/**
 * Starts connecting the person's own account (api/plugins_oauth.go): the
 * browser leaves for the service's sign-in and comes back to /plugins.
 */
export function useConnectPlugin() {
  return useMutation({
    mutationFn: async (id: string) =>
      unwrap(await api.POST('/plugins/{pluginId}/connect', { params: { path: { pluginId: id } } })),
    onSuccess: ({ authorizeUrl }) => window.location.assign(authorizeUrl),
  })
}

/**
 * Connects with the person's own token, for a plugin that takes one
 * (api/plugins_token.go): the server tries it on the service, keeps it sealed
 * and installs the plugin.
 */
export function useSetPluginToken() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async ({ id, token }: { id: string; token: string }): Promise<Plugin> =>
      unwrap(await api.PUT('/plugins/{pluginId}/token', { params: { path: { pluginId: id } }, body: { token } })),
    onSuccess: (plugin) =>
      queryClient.setQueryData<PluginList>(queryKeys.plugins.list(), (held) =>
        held && { ...held, items: held.items.map((item) => (item.id === plugin.id ? plugin : item)) },
      ),
  })
}

export function useUninstallPlugin() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async (id: string) => {
      unwrap(await api.DELETE('/plugins/{pluginId}/install', { params: { path: { pluginId: id } } }))
      return id
    },
    onSuccess: (id) => remember(queryClient, id, false),
  })
}

/**
 * The person's answer to a plugin call waiting for it (api/plugin_code.go):
 * the run that waits goes on by itself and reports on its events stream.
 */
export function useAnswerPluginConfirmation(conversationId: string) {
  return useMutation({
    // Shown in the card itself.
    meta: { silent: true },
    mutationFn: async ({ confirmationId, decision }: { confirmationId: string; decision: 'allow' | 'always' | 'deny' }) =>
      unwrap(
        await api.POST('/conversations/{conversationId}/plugin-confirmations/{confirmationId}', {
          params: { path: { conversationId, confirmationId } },
          body: { decision },
        }),
      ),
  })
}
