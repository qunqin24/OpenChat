import type { components } from '@openchat/api-client'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'

import { adminKeys } from '@/features/admin/use-admin'
import { api, unwrap } from '@/lib/api'
import { queryKeys } from '@openchat/chat-core/query-keys'

type Schemas = components['schemas']
export type AdminPlugin = Schemas['AdminPlugin']
export type AdminPluginInput = Schemas['AdminPluginInput']
export type PluginTool = Schemas['PluginTool']
export type PluginToolPermission = Schemas['PluginToolPermission']

export function useAdminPlugins() {
  return useQuery({
    queryKey: adminKeys.plugins(),
    queryFn: async ({ signal }) => unwrap(await api.GET('/admin/plugins', { signal })),
  })
}

/** Every change answers with the plugin; the list is fetched again, and the people's page too. */
function useRefreshLists() {
  const queryClient = useQueryClient()
  return () => {
    void queryClient.invalidateQueries({ queryKey: adminKeys.plugins() })
    void queryClient.invalidateQueries({ queryKey: queryKeys.plugins.all })
  }
}

export function useSavePlugin() {
  const refresh = useRefreshLists()
  return useMutation({
    mutationFn: async ({ id, body }: { id?: string; body: AdminPluginInput }) =>
      id
        ? unwrap(await api.PUT('/admin/plugins/{pluginId}', { params: { path: { pluginId: id } }, body }))
        : unwrap(await api.POST('/admin/plugins', { body })),
    onSuccess: refresh,
  })
}

export function useDeletePlugin() {
  const refresh = useRefreshLists()
  return useMutation({
    mutationFn: async (id: string) => {
      unwrap(await api.DELETE('/admin/plugins/{pluginId}', { params: { path: { pluginId: id } } }))
    },
    onSuccess: refresh,
  })
}

export function usePluginTools(id: string | undefined) {
  return useQuery({
    queryKey: adminKeys.pluginTools(id ?? ''),
    queryFn: async ({ signal }) =>
      unwrap(await api.GET('/admin/plugins/{pluginId}/tools', { params: { path: { pluginId: id! } }, signal })),
    enabled: Boolean(id),
  })
}

export function useRefreshPluginTools(id: string) {
  const queryClient = useQueryClient()
  const refresh = useRefreshLists()
  return useMutation({
    mutationFn: async () => unwrap(await api.POST('/admin/plugins/{pluginId}/tools/refresh', { params: { path: { pluginId: id } } })),
    onSuccess: (list) => {
      queryClient.setQueryData(adminKeys.pluginTools(id), list)
      refresh()
    },
  })
}

export function useSetToolPermission(id: string) {
  const queryClient = useQueryClient()
  const refresh = useRefreshLists()
  return useMutation({
    mutationFn: async ({ name, permission }: { name: string; permission: PluginToolPermission }) => {
      unwrap(
        await api.PATCH('/admin/plugins/{pluginId}/tools/{toolName}', {
          params: { path: { pluginId: id, toolName: name } },
          body: { permission },
        }),
      )
    },
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: adminKeys.pluginTools(id) })
      refresh()
    },
  })
}
