import { type QueryKey, useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { getRouteApi } from '@tanstack/react-router'
import type { ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { useChatModels } from '@/hooks/use-chat-models'
import { api, unwrap } from '@/lib/api'
import { queryKeys } from '@openchat/chat-core/query-keys'
import { cn } from '@/lib/utils'
import { adminKeys } from '@/features/admin/use-admin'
import { Label } from '@/components/shadcn/ui/label'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/shadcn/ui/select'
import { Skeleton } from '@/components/shadcn/ui/skeleton'
import { Switch } from '@/components/shadcn/ui/switch'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/shadcn/ui/tabs'
import { ModerationPanel } from '@/features/admin/moderation/moderation-panel'
import { ContentSection } from '../components/content-section'
import { RetentionForm } from './retention-form'
import { RunLimitForm } from './run-limit-form'
import { SubagentsPanel } from './subagents-panel'
import { SYSTEM_TABS, type SystemTab } from './tabs'
import { VersionRow } from './version-row'

const route = getRouteApi('/admin/settings/system')

/** The task model's select value for "each conversation's own" (Radix Select takes no empty value). */
const OWN_MODEL = '__conversation__'

/**
 * Settings that belong to the server rather than to this browser — which is
 * what separates this section from Appearance next door.
 */
export function SettingsSystem() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const { tab = 'general' } = route.useSearch()
  const navigate = route.useNavigate()
  const setTab = (next: SystemTab) => void navigate({ search: { tab: next }, replace: true })

  const { data, isPending } = useQuery({
    queryKey: adminKeys.system(),
    queryFn: async ({ signal }) => unwrap(await api.GET('/admin/system', { signal })),
  })

  const toggleGenUI = useSystemToggle('generativeUi', 'genui')
  const toggleAskUser = useSystemToggle('askUser', 'ask-user')
  const toggleMemory = useSystemToggle('memory', 'memory')
  // The chat's sidebar offers the tasks page from the features.
  const toggleTasks = useSystemToggle('scheduledTasks', 'tasks', [queryKeys.features])
  // The chat's top bar offers 轨迹 from the features.
  const toggleTraces = useSystemToggle('requestTraces', 'traces', [queryKeys.features])
  const toggleCompress = useSystemToggle('compressContext', 'compress-context')
  // The account settings read it from the chat's features.
  const toggleAvatars = useSystemToggle('avatarUploads', 'avatars', [queryKeys.features])
  // The sign-in pages offer the register link from these.
  const toggleRegistration = useSystemToggle('allowRegistration', 'allow-registration', [queryKeys.auth.options])
  const toggleApproval = useSystemToggle('registrationApproval', 'approval', [queryKeys.auth.options])
  const toggleDebug = useSystemToggle('debugLogging', '')

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


  const loading = (width = 'w-8', height = 'h-5') => <Skeleton className={`${height} ${width} shrink-0`} />
  const toggle = (id: string, checked: boolean, mutation: { isPending: boolean; mutate: (v: boolean) => void }) =>
    isPending ? (
      loading()
    ) : (
      <Switch
        id={id}
        className="mt-0.5 shrink-0"
        checked={checked}
        disabled={mutation.isPending}
        onCheckedChange={(value) => mutation.mutate(value)}
      />
    )

  return (
    <ContentSection title={t('admin.settings.system.title')} desc={t('admin.settings.system.subtitle')}>
      <Tabs value={tab} onValueChange={(value) => setTab(value as SystemTab)}>
        <TabsList>
          {SYSTEM_TABS.map((value) => (
            <TabsTrigger key={value} value={value}>
              {t(`admin.settings.system.tabs.${value}`)}
            </TabsTrigger>
          ))}
        </TabsList>

        <TabsContent value="general" className="divide-y">
          <VersionRow />
          <SettingRow id="task-model" label={t('admin.settings.system.task-model')} hint={t('admin.settings.system.task-model-hint')}>
            {isPending || models.isPending ? (
              loading('w-48', 'h-9')
            ) : (
              <Select
                value={taskModel || OWN_MODEL}
                disabled={setTaskModel.isPending}
                onValueChange={(value) => setTaskModel.mutate(value === OWN_MODEL ? '' : value)}
              >
                <SelectTrigger id="task-model" className="w-48 shrink-0">
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
          </SettingRow>
          <SettingRow id="debug-logging" label={t('admin.settings.system.debug')} hint={t('admin.settings.system.debug-hint')}>
            {toggle('debug-logging', data?.debugLogging ?? false, toggleDebug)}
          </SettingRow>
        </TabsContent>

        <TabsContent value="chat" className="divide-y">
          <SettingRow id="generative-ui" label={t('admin.settings.system.genui')} hint={t('admin.settings.system.genui-hint')}>
            {toggle('generative-ui', data?.generativeUi ?? false, toggleGenUI)}
          </SettingRow>
          <SettingRow id="ask-user" label={t('admin.settings.system.ask-user')} hint={t('admin.settings.system.ask-user-hint')}>
            {toggle('ask-user', data?.askUser ?? true, toggleAskUser)}
          </SettingRow>
          <SettingRow id="memory" label={t('admin.settings.system.memory')} hint={t('admin.settings.system.memory-hint')}>
            {toggle('memory', data?.memory ?? true, toggleMemory)}
          </SettingRow>
          <SettingRow id="scheduled-tasks" label={t('admin.settings.system.tasks')} hint={t('admin.settings.system.tasks-hint')}>
            {toggle('scheduled-tasks', data?.scheduledTasks ?? true, toggleTasks)}
          </SettingRow>
          <SettingRow id="request-traces" label={t('admin.settings.system.traces')} hint={t('admin.settings.system.traces-hint')}>
            {toggle('request-traces', data?.requestTraces ?? false, toggleTraces)}
          </SettingRow>
          <SettingRow
            id="compress-context"
            label={t('admin.settings.system.compress-context')}
            hint={t('admin.settings.system.compress-context-hint')}
          >
            {toggle('compress-context', data?.compressContext ?? true, toggleCompress)}
          </SettingRow>
          {/* One form of its own, like the other numbers: it is saved with a button, not on change. */}
          <div className="py-4">
            <div className="space-y-1">
              <div className="text-sm font-medium">{t('admin.settings.system.run-limit.title')}</div>
              <p className="text-muted-foreground text-sm">{t('admin.settings.system.run-limit.intro')}</p>
            </div>
            {isPending || !data ? <Skeleton className="my-4 h-28 w-full" /> : <RunLimitForm settings={data} />}
          </div>
        </TabsContent>

        <TabsContent value="subagents">
          <SubagentsPanel />
        </TabsContent>

        <TabsContent value="access" className="divide-y">
          <SettingRow
            id="allow-registration"
            label={t('admin.settings.system.allow-registration')}
            hint={t('admin.settings.system.allow-registration-hint')}
          >
            {toggle('allow-registration', data?.allowRegistration ?? true, toggleRegistration)}
          </SettingRow>
          {/* Only matters while registration is allowed: dimmed and unclickable
              otherwise, still showing what it will be once registration is back. */}
          <SettingRow
            id="registration-approval"
            label={t('admin.settings.system.approval')}
            hint={t('admin.settings.system.approval-hint')}
            className={data && !data.allowRegistration ? 'opacity-50' : undefined}
          >
            {isPending ? (
              loading()
            ) : (
              <Switch
                id="registration-approval"
                className="mt-0.5 shrink-0"
                checked={data?.registrationApproval ?? true}
                disabled={toggleApproval.isPending || !data?.allowRegistration}
                onCheckedChange={(value) => toggleApproval.mutate(value)}
              />
            )}
          </SettingRow>
          <SettingRow id="avatar-uploads" label={t('admin.settings.system.avatars')} hint={t('admin.settings.system.avatars-hint')}>
            {toggle('avatar-uploads', data?.avatarUploads ?? false, toggleAvatars)}
          </SettingRow>
        </TabsContent>

        <TabsContent value="moderation">
          <ModerationPanel />
        </TabsContent>

        <TabsContent value="data">
          {isPending || !data ? <Skeleton className="my-4 h-72 w-full" /> : <RetentionForm settings={data} />}
        </TabsContent>
      </Tabs>
    </ContentSection>
  )
}

/**
 * One setting as a line of the list: what it is on the left, its control on
 * the right, the lines divided by rules rather than each boxed in a card.
 */
function SettingRow({
  id,
  label,
  hint,
  className,
  children,
}: {
  id: string
  label: string
  hint: string
  className?: string
  children: ReactNode
}) {
  return (
    <div className={cn('flex items-start justify-between gap-6 py-4', className)}>
      <div className="space-y-1">
        <Label htmlFor={id} className="text-sm font-medium">
          {label}
        </Label>
        <p className="text-muted-foreground text-sm">{hint}</p>
      </div>
      {children}
    </div>
  )
}

type SystemFlag =
  | 'generativeUi'
  | 'askUser'
  | 'memory'
  | 'scheduledTasks'
  | 'requestTraces'
  | 'avatarUploads'
  | 'debugLogging'
  | 'allowRegistration'
  | 'registrationApproval'
  | 'compressContext'

/**
 * A switch saved on its own, as each of these is: the server answers with the
 * whole settings, which replace the cached ones. `message` names the toasts
 * (`<message>-on` / `-off`); debug logging's are the bare `on` / `off`.
 */
function useSystemToggle(flag: SystemFlag, message: string, alsoRefresh: QueryKey[] = []) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async (value: boolean) => unwrap(await api.PATCH('/admin/system', { body: { [flag]: value } })),
    onSuccess: (next) => {
      queryClient.setQueryData(adminKeys.system(), next)
      for (const key of alsoRefresh) void queryClient.invalidateQueries({ queryKey: key })
      const prefix = message ? `${message}-` : ''
      toast.success(t(`admin.settings.system.${prefix}${next[flag] ? 'on' : 'off'}` as 'admin.settings.system.on'))
    },
    onError: () => toast.error(t('admin.settings.system.save-failed')),
  })
}

export default SettingsSystem
