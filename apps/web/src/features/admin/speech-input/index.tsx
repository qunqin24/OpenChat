import { isOpenChatError, type components } from '@openchat/api-client'
import { zodResolver } from '@hookform/resolvers/zod'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import { TriangleAlert } from 'lucide-react'
import { useForm, useWatch } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { z } from 'zod'

import { ConfigDrawer } from '@/components/shadcn/config-drawer'
import { Header } from '@/components/shadcn/layout/header'
import { Main } from '@/components/shadcn/layout/main'
import { Search } from '@/components/shadcn/search'
import { Alert, AlertTitle } from '@/components/shadcn/ui/alert'
import { Button } from '@/components/shadcn/ui/button'
import { Form, FormControl, FormDescription, FormField, FormItem, FormLabel, FormMessage } from '@/components/shadcn/ui/form'
import { Input } from '@/components/shadcn/ui/input'
import { Label } from '@/components/shadcn/ui/label'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/shadcn/ui/select'
import { Separator } from '@/components/shadcn/ui/separator'
import { Skeleton } from '@/components/shadcn/ui/skeleton'
import { Switch } from '@/components/shadcn/ui/switch'
import { adminKeys } from '@/features/admin/use-admin'
import { api, unwrap } from '@/lib/api'
import { queryKeys } from '@openchat/chat-core/query-keys'

type SpeechSettings = components['schemas']['SpeechInputSettings']
type UpdateSpeechSettings = components['schemas']['UpdateSpeechInputSettingsRequest']

function useSpeechSettings() {
  return useQuery({
    queryKey: adminKeys.speechInput(),
    queryFn: async ({ signal }) => unwrap(await api.GET('/admin/speech-input', { signal })),
  })
}

function useUpdateSpeechSettings() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async (body: UpdateSpeechSettings) => unwrap(await api.PATCH('/admin/speech-input', { body })),
    onSuccess: (next) => {
      queryClient.setQueryData(adminKeys.speechInput(), next)
      // Whether the composer shows a microphone is part of what the chat is told it can do.
      void queryClient.invalidateQueries({ queryKey: queryKeys.features })
    },
  })
}

/** The speech-to-text models a channel serves: what the model can be chosen from. */
function useSpeechModels() {
  return useQuery({
    queryKey: adminKeys.speechModels(),
    queryFn: async ({ signal }) => {
      const page = unwrap(await api.GET('/admin/catalog/models', { params: { query: { limit: 100 } }, signal }))
      return page.items.filter((model) => model.settings?.kind === 'transcription')
    },
  })
}

/** What a failed save says: the server's reason for settings it refused. */
function saveError(error: unknown, fallback: string) {
  return isOpenChatError(error) && error.code === 'invalid_speech_input_settings' ? error.message : fallback
}

/**
 * Speech input (api/speech_input.go, docs/design/speech-input-plan.md): on or
 * off for everyone, the speech-to-text model recordings are transcribed with,
 * and the longest recording. Laid out as the image generation page is.
 */
export function SpeechInputPage() {
  const { t } = useTranslation()
  const { data, isPending } = useSpeechSettings()

  return (
    <>
      <Header fixed>
        <Search className="me-auto" />
        <ConfigDrawer />
      </Header>

      <Main className="flex flex-1 flex-col">
        <div>
          <h2 className="text-2xl font-bold tracking-tight">{t('admin.speech-input.title')}</h2>
          <p className="text-muted-foreground">{t('admin.speech-input.subtitle')}</p>
        </div>
        <Separator className="my-4 lg:my-6" />

        {isPending || !data ? (
          <div className="max-w-2xl space-y-4">
            <Skeleton className="h-20 w-full" />
            <Skeleton className="h-56 w-full" />
          </div>
        ) : (
          <div className="max-w-2xl space-y-10 pb-12">
            <EnabledSwitch settings={data} />
            <SettingsForm settings={data} />
          </div>
        )}
      </Main>
    </>
  )
}

export default SpeechInputPage

function EnabledSwitch({ settings }: { settings: SpeechSettings }) {
  const { t } = useTranslation()
  const update = useUpdateSpeechSettings()
  return (
    <div className="flex flex-row items-center justify-between gap-6 rounded-lg border p-4">
      <div className="space-y-0.5">
        <Label htmlFor="speech-enabled" className="text-base">
          {t('admin.speech-input.enabled')}
        </Label>
        <p className="text-muted-foreground text-sm">{t('admin.speech-input.enabled-hint')}</p>
      </div>
      <Switch
        id="speech-enabled"
        checked={settings.enabled}
        disabled={update.isPending}
        onCheckedChange={(enabled) =>
          update.mutate(
            { enabled },
            {
              onSuccess: () => toast.success(t(enabled ? 'admin.speech-input.turned-on' : 'admin.speech-input.turned-off')),
              onError: (error) => toast.error(saveError(error, t('admin.speech-input.save-failed'))),
            },
          )
        }
      />
    </div>
  )
}

function SettingsForm({ settings }: { settings: SpeechSettings }) {
  const { t } = useTranslation()
  const update = useUpdateSpeechSettings()
  const models = useSpeechModels()

  const schema = z.object({
    model: z.string(),
    maxSeconds: z.coerce
      .number<number>()
      .int(t('admin.speech-input.max-seconds-range'))
      .min(10, t('admin.speech-input.max-seconds-range'))
      .max(1800, t('admin.speech-input.max-seconds-range')),
  })
  type Values = z.infer<typeof schema>

  const form = useForm<z.input<typeof schema>, unknown, Values>({
    resolver: zodResolver(schema),
    values: { model: settings.model, maxSeconds: settings.maxSeconds },
  })
  const chosen = useWatch({ control: form.control, name: 'model' })

  const save = (values: Values) =>
    update.mutate(values, {
      onSuccess: () => toast.success(t('admin.speech-input.saved')),
      onError: (error) => toast.error(saveError(error, t('admin.speech-input.save-failed'))),
    })

  const list = models.data ?? []
  return (
    <Form {...form}>
      <form className="space-y-6" onSubmit={form.handleSubmit(save)}>
          <FormField
            control={form.control}
            name="model"
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('admin.speech-input.model')}</FormLabel>
                <Select value={field.value || undefined} onValueChange={field.onChange} disabled={models.isPending}>
                  <FormControl>
                    <SelectTrigger className="w-full max-w-md">
                      <SelectValue placeholder={t(list.length ? 'admin.speech-input.model-placeholder' : 'admin.speech-input.model-none')} />
                    </SelectTrigger>
                  </FormControl>
                  <SelectContent>
                    {/* The saved model stays listed even if no channel serves it now. */}
                    {field.value && !list.some((m) => m.modelId === field.value) && <SelectItem value={field.value}>{field.value}</SelectItem>}
                    {list.map((model) => (
                      <SelectItem key={model.modelId} value={model.modelId}>
                        {model.name}
                        {model.name !== model.modelId && <span className="text-muted-foreground ms-2 font-mono text-xs">{model.modelId}</span>}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
                <FormDescription>
                  {t('admin.speech-input.model-hint')}{' '}
                  <Link to="/admin/models" className="underline underline-offset-4">
                    {t('admin.speech-input.model-hint-link')}
                  </Link>
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />
          {chosen && chosen === settings.model && !settings.modelPriced && (
            <Alert>
              <TriangleAlert aria-hidden="true" />
              <AlertTitle>
                {t('admin.speech-input.unpriced')}{' '}
                <Link to="/admin/models" className="underline underline-offset-4">
                  {t('admin.speech-input.unpriced-link')}
                </Link>
              </AlertTitle>
            </Alert>
          )}

          <FormField
            control={form.control}
            name="maxSeconds"
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('admin.speech-input.maxSeconds')}</FormLabel>
                <FormControl>
                  <Input type="number" inputMode="numeric" className="w-32" {...field} value={field.value as number | string} />
                </FormControl>
                <FormDescription>{t('admin.speech-input.maxSeconds-hint')}</FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />

        <Button type="submit" disabled={update.isPending || !form.formState.isDirty}>
          {t('admin.speech-input.save')}
        </Button>
      </form>
    </Form>
  )
}
