import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { useChatModels } from '@/hooks/use-chat-models'
import { api, unwrap } from '@/lib/api'
import { adminKeys } from '@/features/admin/use-admin'
import { Label } from '@/components/shadcn/ui/label'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/shadcn/ui/select'
import { Skeleton } from '@/components/shadcn/ui/skeleton'
import { Switch } from '@/components/shadcn/ui/switch'
import { ContentSection } from '../components/content-section'

/** The task model's select value for "each conversation's own" (Radix Select takes no empty value). */
const OWN_MODEL = '__conversation__'

/**
 * Settings that belong to the server rather than to this browser — which is
 * what separates this section from Appearance next door.
 */
export function SettingsSystem() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()

  const { data, isPending } = useQuery({
    queryKey: adminKeys.system(),
    queryFn: async ({ signal }) => unwrap(await api.GET('/admin/system', { signal })),
  })

  const toggleGenUI = useMutation({
    mutationFn: async (generativeUi: boolean) =>
      unwrap(await api.PATCH('/admin/system', { body: { generativeUi } })),
    onSuccess: (next) => {
      queryClient.setQueryData(adminKeys.system(), next)
      toast.success(t(next.generativeUi ? 'admin.settings.system.genui-on' : 'admin.settings.system.genui-off'))
    },
    onError: () => toast.error(t('admin.settings.system.save-failed')),
  })

  const toggleAskUser = useMutation({
    mutationFn: async (askUser: boolean) => unwrap(await api.PATCH('/admin/system', { body: { askUser } })),
    onSuccess: (next) => {
      queryClient.setQueryData(adminKeys.system(), next)
      toast.success(t(next.askUser ? 'admin.settings.system.ask-user-on' : 'admin.settings.system.ask-user-off'))
    },
    onError: () => toast.error(t('admin.settings.system.save-failed')),
  })

  const toggleMemory = useMutation({
    mutationFn: async (memory: boolean) => unwrap(await api.PATCH('/admin/system', { body: { memory } })),
    onSuccess: (next) => {
      queryClient.setQueryData(adminKeys.system(), next)
      toast.success(t(next.memory ? 'admin.settings.system.memory-on' : 'admin.settings.system.memory-off'))
    },
    onError: () => toast.error(t('admin.settings.system.save-failed')),
  })

  // What enabled channels offer — the same list the chat's model picker shows.
  const models = useChatModels()
  const setTaskModel = useMutation({
    mutationFn: async (taskModel: string) =>
      unwrap(await api.PATCH('/admin/system', { body: { taskModel } })),
    onSuccess: (next) => {
      queryClient.setQueryData(adminKeys.system(), next)
      toast.success(t('admin.settings.system.task-model-saved'))
    },
    onError: () => toast.error(t('admin.settings.system.save-failed')),
  })
  // A model that is no longer offered still shows, so the setting reads as
  // what is stored rather than silently as something else.
  const taskModel = data?.taskModel ?? ''
  const offered = models.data ?? []
  const stale = taskModel !== '' && !offered.some((m) => m.id === taskModel)

  const update = useMutation({
    mutationFn: async (debugLogging: boolean) =>
      unwrap(await api.PATCH('/admin/system', { body: { debugLogging } })),
    onSuccess: (next) => {
      queryClient.setQueryData(adminKeys.system(), next)
      toast.success(t(next.debugLogging ? 'admin.settings.system.on' : 'admin.settings.system.off'))
    },
    onError: () => toast.error(t('admin.settings.system.save-failed')),
  })

  return (
    <ContentSection
      title={t('admin.settings.system.title')}
      desc={t('admin.settings.system.subtitle')}
    >
      <div className="space-y-4">
        <div className="flex items-start justify-between gap-6 rounded-lg border p-4">
          <div className="space-y-1">
            <Label htmlFor="debug-logging" className="text-base">
              {t('admin.settings.system.debug')}
            </Label>
            <p className="text-muted-foreground text-sm">
              {t('admin.settings.system.debug-hint')}
            </p>
          </div>

          {isPending ? (
            <Skeleton className="h-5 w-8 shrink-0" />
          ) : (
            <Switch
              id="debug-logging"
              className="mt-1 shrink-0"
              checked={data?.debugLogging ?? false}
              disabled={update.isPending}
              onCheckedChange={(value) => update.mutate(value)}
            />
          )}
        </div>

        <div className="flex items-start justify-between gap-6 rounded-lg border p-4">
          <div className="space-y-1">
            <Label htmlFor="generative-ui" className="text-base">
              {t('admin.settings.system.genui')}
            </Label>
            <p className="text-muted-foreground text-sm">{t('admin.settings.system.genui-hint')}</p>
          </div>
          {isPending ? (
            <Skeleton className="h-5 w-8 shrink-0" />
          ) : (
            <Switch
              id="generative-ui"
              className="mt-1 shrink-0"
              checked={data?.generativeUi ?? false}
              disabled={toggleGenUI.isPending}
              onCheckedChange={(value) => toggleGenUI.mutate(value)}
            />
          )}
        </div>

        <div className="flex items-start justify-between gap-6 rounded-lg border p-4">
          <div className="space-y-1">
            <Label htmlFor="ask-user" className="text-base">
              {t('admin.settings.system.ask-user')}
            </Label>
            <p className="text-muted-foreground text-sm">{t('admin.settings.system.ask-user-hint')}</p>
          </div>
          {isPending ? (
            <Skeleton className="h-5 w-8 shrink-0" />
          ) : (
            <Switch
              id="ask-user"
              className="mt-1 shrink-0"
              checked={data?.askUser ?? true}
              disabled={toggleAskUser.isPending}
              onCheckedChange={(value) => toggleAskUser.mutate(value)}
            />
          )}
        </div>

        <div className="flex items-start justify-between gap-6 rounded-lg border p-4">
          <div className="space-y-1">
            <Label htmlFor="memory" className="text-base">
              {t('admin.settings.system.memory')}
            </Label>
            <p className="text-muted-foreground text-sm">{t('admin.settings.system.memory-hint')}</p>
          </div>
          {isPending ? (
            <Skeleton className="h-5 w-8 shrink-0" />
          ) : (
            <Switch
              id="memory"
              className="mt-1 shrink-0"
              checked={data?.memory ?? true}
              disabled={toggleMemory.isPending}
              onCheckedChange={(value) => toggleMemory.mutate(value)}
            />
          )}
        </div>

        <div className="flex items-start justify-between gap-6 rounded-lg border p-4">
          <div className="space-y-1">
            <Label htmlFor="task-model" className="text-base">
              {t('admin.settings.system.task-model')}
            </Label>
            <p className="text-muted-foreground text-sm">{t('admin.settings.system.task-model-hint')}</p>
          </div>
          {isPending || models.isPending ? (
            <Skeleton className="h-9 w-56 shrink-0" />
          ) : (
            <Select
              value={taskModel || OWN_MODEL}
              disabled={setTaskModel.isPending}
              onValueChange={(value) => setTaskModel.mutate(value === OWN_MODEL ? '' : value)}
            >
              <SelectTrigger id="task-model" className="w-56 shrink-0">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value={OWN_MODEL}>{t('admin.settings.system.task-model-own')}</SelectItem>
                {stale && (
                  <SelectItem value={taskModel}>
                    {t('admin.settings.system.task-model-gone', { model: taskModel })}
                  </SelectItem>
                )}
                {offered.map((model) => (
                  <SelectItem key={model.id} value={model.id}>
                    {model.name}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          )}
        </div>
      </div>
    </ContentSection>
  )
}

export default SettingsSystem
