import { zodResolver } from '@hookform/resolvers/zod'
import type { SubagentSettings, UpdateSubagentSettingsRequest } from '@openchat/api-client'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { z } from 'zod'

import { Button } from '@/components/shadcn/ui/button'
import { Form, FormControl, FormField, FormItem, FormLabel, FormMessage } from '@/components/shadcn/ui/form'
import { Input } from '@/components/shadcn/ui/input'
import { Label } from '@/components/shadcn/ui/label'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/shadcn/ui/select'
import { Skeleton } from '@/components/shadcn/ui/skeleton'
import { Switch } from '@/components/shadcn/ui/switch'
import { adminKeys } from '@/features/admin/use-admin'
import { useChatModels } from '@/hooks/use-chat-models'
import { api, unwrap } from '@/lib/api'
import { queryKeys } from '@openchat/chat-core/query-keys'

/** The model select's value for "the main model's" (Radix Select takes no empty value). */
const MAIN_MODEL = '__main__'

/**
 * 子智能体: whether models may hand work to sub-agents, which model those use,
 * whether the main agent may leave them working in the background, and how
 * many may go at once (docs/design/sub-agents.md §8). Off by default.
 */
export function SubagentsPanel() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const { data, isPending } = useQuery({
    queryKey: adminKeys.subagents(),
    queryFn: async ({ signal }) => unwrap(await api.GET('/admin/subagents', { signal })),
  })
  const models = useChatModels()
  const save = useMutation({
    mutationFn: async (body: UpdateSubagentSettingsRequest) => unwrap(await api.PATCH('/admin/subagents', { body })),
    onSuccess: (next) => {
      queryClient.setQueryData(adminKeys.subagents(), next)
      // The chat reads whether to show sub-agents from its features.
      void queryClient.invalidateQueries({ queryKey: queryKeys.features })
      toast.success(t('admin.settings.system.subagents.saved'))
    },
    onError: () => toast.error(t('admin.settings.system.save-failed')),
  })

  if (isPending || !data) return <Skeleton className="my-4 h-72 w-full" />

  const offered = models.data ?? []
  const stale = data.model !== '' && !offered.some((m) => m.id === data.model)

  return (
    <div className="divide-y">
      <Row id="subagents-on" label={t('admin.settings.system.subagents.enabled')} hint={t('admin.settings.system.subagents.enabled-hint')}>
        <Switch
          id="subagents-on"
          className="mt-0.5 shrink-0"
          checked={data.enabled}
          disabled={save.isPending}
          onCheckedChange={(enabled) => save.mutate({ enabled })}
        />
      </Row>
      <Row id="subagents-model" label={t('admin.settings.system.subagents.model')} hint={t('admin.settings.system.subagents.model-hint')}>
        <Select
          value={data.model || MAIN_MODEL}
          disabled={save.isPending || models.isPending}
          onValueChange={(value) => save.mutate({ model: value === MAIN_MODEL ? '' : value })}
        >
          <SelectTrigger id="subagents-model" className="w-48 shrink-0">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value={MAIN_MODEL}>{t('admin.settings.system.subagents.model-main')}</SelectItem>
            {stale && <SelectItem value={data.model}>{t('admin.settings.system.task-model-gone', { model: data.model })}</SelectItem>}
            {offered.map((model) => (
              <SelectItem key={model.id} value={model.id}>
                {model.name}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </Row>
      <Row
        id="subagents-background"
        label={t('admin.settings.system.subagents.background')}
        hint={t('admin.settings.system.subagents.background-hint')}
      >
        <Switch
          id="subagents-background"
          className="mt-0.5 shrink-0"
          checked={data.background}
          disabled={save.isPending}
          onCheckedChange={(background) => save.mutate({ background })}
        />
      </Row>
      <LimitsForm settings={data} saving={save.isPending} onSave={(body) => save.mutate(body)} />
    </div>
  )
}

/** The numbers, saved together with a button as the other limits are. */
function LimitsForm({
  settings,
  saving,
  onSave,
}: {
  settings: SubagentSettings
  saving: boolean
  onSave: (body: UpdateSubagentSettingsRequest) => void
}) {
  const { t } = useTranslation()
  const field = (min: number, max: number) =>
    z
      .union([z.string(), z.number()])
      .refine((value) => String(value).trim() !== '', { error: t('admin.settings.system.subagents.required') })
      .transform((value) => Number(String(value).trim()))
      .pipe(
        z
          .number({ error: t('admin.settings.system.subagents.whole') })
          .int(t('admin.settings.system.subagents.whole'))
          .min(min, t('admin.settings.system.subagents.range', { min, max }))
          .max(max, t('admin.settings.system.subagents.range', { min, max })),
      )
  const schema = z.object({
    maxPerRun: field(1, 20),
    maxPerConversation: field(1, 20),
    maxPerUser: field(1, 100),
    maxReportChars: field(1000, 200000),
  })
  type Values = z.infer<typeof schema>
  const form = useForm<z.input<typeof schema>, unknown, Values>({
    resolver: zodResolver(schema),
    values: {
      maxPerRun: settings.maxPerRun,
      maxPerConversation: settings.maxPerConversation,
      maxPerUser: settings.maxPerUser,
      maxReportChars: settings.maxReportChars,
    },
  })
  const fields = [
    { name: 'maxPerRun', label: t('admin.settings.system.subagents.per-run') },
    { name: 'maxPerConversation', label: t('admin.settings.system.subagents.per-conversation') },
    { name: 'maxPerUser', label: t('admin.settings.system.subagents.per-user') },
    { name: 'maxReportChars', label: t('admin.settings.system.subagents.report-chars') },
  ] as const

  return (
    <div className="space-y-4 py-4">
      <div className="space-y-1">
        <div className="text-sm font-medium">{t('admin.settings.system.subagents.limits')}</div>
        <p className="text-muted-foreground text-sm">{t('admin.settings.system.subagents.limits-hint')}</p>
      </div>
      <Form {...form}>
        <form className="space-y-4" onSubmit={form.handleSubmit((values) => onSave(values))}>
          <div className="grid gap-4 sm:grid-cols-2">
            {fields.map(({ name, label }) => (
              <FormField
                key={name}
                control={form.control}
                name={name}
                render={({ field: input }) => (
                  <FormItem>
                    <FormLabel>{label}</FormLabel>
                    <FormControl>
                      <Input type="number" inputMode="numeric" className="w-36" {...input} value={input.value as number | string} />
                    </FormControl>
                    <FormMessage />
                  </FormItem>
                )}
              />
            ))}
          </div>
          <Button type="submit" disabled={saving || !form.formState.isDirty}>
            {t('admin.settings.system.subagents.save')}
          </Button>
        </form>
      </Form>
    </div>
  )
}

function Row({ id, label, hint, children }: { id: string; label: string; hint: string; children: React.ReactNode }) {
  return (
    <div className="flex items-start justify-between gap-6 py-4">
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
