import { isOpenChatError, type components } from '@openchat/api-client'
import { zodResolver } from '@hookform/resolvers/zod'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import { CheckCircle2, Loader2, TriangleAlert, XCircle } from 'lucide-react'
import { useState, type ReactNode } from 'react'
import { useForm, useWatch } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { z } from 'zod'

import { ConfigDrawer } from '@/components/shadcn/config-drawer'
import { Header } from '@/components/shadcn/layout/header'
import { Main } from '@/components/shadcn/layout/main'
import { ProfileDropdown } from '@/components/shadcn/profile-dropdown'
import { Search } from '@/components/shadcn/search'
import { Alert, AlertDescription, AlertTitle } from '@/components/shadcn/ui/alert'
import { Badge } from '@/components/shadcn/ui/badge'
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

type ImageSettings = components['schemas']['ImageGenerationSettings']
type ImageQuality = components['schemas']['ImageQuality']
type ImageComplexity = components['schemas']['ImageComplexity']

/** The planning levels, after auto (the provider's default). */
const COMPLEXITIES: ImageComplexity[] = ['auto', 'low', 'medium', 'high']

/** The levels, lowest first, after auto (the provider's default, or no cap). */
const QUALITIES: ImageQuality[] = ['auto', 'low', 'medium', 'high', 'xhigh', 'max']
type UpdateImageSettings = components['schemas']['UpdateImageGenerationSettingsRequest']

function useImageSettings() {
  return useQuery({
    queryKey: adminKeys.imageGeneration(),
    queryFn: async ({ signal }) => unwrap(await api.GET('/admin/image-generation', { signal })),
  })
}

function useUpdateImageSettings() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async (body: UpdateImageSettings) => unwrap(await api.PATCH('/admin/image-generation', { body })),
    onSuccess: (next) => {
      queryClient.setQueryData(adminKeys.imageGeneration(), next)
      // Whether pictures can be made is part of what the chat is told it can do.
      void queryClient.invalidateQueries({ queryKey: queryKeys.features })
    },
  })
}

/** The image models a channel serves: what the model can be chosen from. */
function useImageModels() {
  return useQuery({
    queryKey: adminKeys.imageModels(),
    queryFn: async ({ signal }) => {
      const page = unwrap(await api.GET('/admin/catalog/models', { params: { query: { limit: 100 } }, signal }))
      return page.items.filter((model) => model.settings?.kind === 'image')
    },
  })
}

/** What a failed save says: the server's reason for settings it refused. */
function saveError(error: unknown, fallback: string) {
  return isOpenChatError(error) && error.code === 'invalid_image_settings' ? error.message : fallback
}

/**
 * Image generation (api/image_settings.go, docs/design/image-generation-plan.md):
 * on or off for everyone, the image model the pictures are made with, the
 * limits, and a picture to try. Laid out as the browser page is (design 1).
 */
export function ImageGenerationPage() {
  const { t } = useTranslation()
  const { data, isPending } = useImageSettings()

  return (
    <>
      <Header fixed>
        <Search className="me-auto" />
        <ConfigDrawer />
        <ProfileDropdown />
      </Header>

      <Main className="flex flex-1 flex-col">
        <div>
          <h2 className="text-2xl font-bold tracking-tight">{t('admin.image-generation.title')}</h2>
          <p className="text-muted-foreground">{t('admin.image-generation.subtitle')}</p>
        </div>
        <Separator className="my-4 lg:my-6" />

        {isPending || !data ? (
          <div className="max-w-2xl space-y-4">
            <Skeleton className="h-20 w-full" />
            <Skeleton className="h-72 w-full" />
          </div>
        ) : (
          <div className="max-w-2xl space-y-10 pb-12">
            <EnabledSwitch settings={data} />
            <SettingsForm settings={data} />
            <Section title={t('admin.image-generation.test')} description={t('admin.image-generation.test-hint')}>
              <TryPicture disabled={!data.model} />
            </Section>
          </div>
        )}
      </Main>
    </>
  )
}

export default ImageGenerationPage

function Section({ title, description, children }: { title: string; description: string; children: ReactNode }) {
  return (
    <section className="space-y-6">
      <div>
        <h3 className="text-lg font-medium">{title}</h3>
        <p className="text-muted-foreground text-sm">{description}</p>
      </div>
      {children}
    </section>
  )
}

function EnabledSwitch({ settings }: { settings: ImageSettings }) {
  const { t } = useTranslation()
  const update = useUpdateImageSettings()
  return (
    <div className="flex flex-row items-center justify-between gap-6 rounded-lg border p-4">
      <div className="space-y-0.5">
        <div className="flex items-center gap-2">
          <Label htmlFor="images-enabled" className="text-base">
            {t('admin.image-generation.enabled')}
          </Label>
          <Badge variant={settings.enabled ? 'default' : 'secondary'}>
            {settings.enabled ? t('admin.image-generation.status-on') : t('admin.image-generation.status-off')}
          </Badge>
        </div>
        <p className="text-muted-foreground text-sm">{t('admin.image-generation.enabled-hint')}</p>
      </div>
      <Switch
        id="images-enabled"
        checked={settings.enabled}
        disabled={update.isPending}
        onCheckedChange={(enabled) =>
          update.mutate(
            { enabled },
            {
              onSuccess: () => toast.success(t(enabled ? 'admin.image-generation.turned-on' : 'admin.image-generation.turned-off')),
              onError: (error) => toast.error(saveError(error, t('admin.image-generation.save-failed'))),
            },
          )
        }
      />
    </div>
  )
}

function SettingsForm({ settings }: { settings: ImageSettings }) {
  const { t } = useTranslation()
  const update = useUpdateImageSettings()
  const models = useImageModels()

  const whole = (min: number, max: number, message: string) => z.coerce.number<number>().int(message).min(min, message).max(max, message)
  const schema = z
    .object({
      model: z.string(),
      maxPerCall: whole(1, 10, t('admin.image-generation.per-call-range')),
      maxPerRun: whole(1, 50, t('admin.image-generation.per-run-range')),
      daily: whole(0, 100000, t('admin.image-generation.daily-range')),
      defaultQuality: z.enum(QUALITIES),
      maxQuality: z.enum(QUALITIES),
      qualityFixed: z.boolean(),
      defaultComplexity: z.enum(COMPLEXITIES),
      complexityFixed: z.boolean(),
    })
    .refine((v) => v.maxPerRun >= v.maxPerCall, { path: ['maxPerRun'], message: t('admin.image-generation.per-run-below') })
    .refine(
      (v) =>
        v.qualityFixed || v.defaultQuality === 'auto' || v.maxQuality === 'auto' || QUALITIES.indexOf(v.defaultQuality) <= QUALITIES.indexOf(v.maxQuality),
      { path: ['defaultQuality'], message: t('admin.image-generation.quality-above-max') },
    )
  type Values = z.infer<typeof schema>

  const form = useForm<z.input<typeof schema>, unknown, Values>({
    resolver: zodResolver(schema),
    values: {
      model: settings.model,
      maxPerCall: settings.maxPerCall,
      maxPerRun: settings.maxPerRun,
      daily: settings.daily,
      defaultQuality: settings.defaultQuality,
      maxQuality: settings.maxQuality,
      qualityFixed: settings.qualityFixed,
      defaultComplexity: settings.defaultComplexity,
      complexityFixed: settings.complexityFixed,
    },
  })
  const chosen = useWatch({ control: form.control, name: 'model' })
  const qualityFixed = useWatch({ control: form.control, name: 'qualityFixed' })
  const complexityFixed = useWatch({ control: form.control, name: 'complexityFixed' })

  const save = (values: Values) =>
    update.mutate(values, {
      onSuccess: () => toast.success(t('admin.image-generation.saved')),
      onError: (error) => toast.error(saveError(error, t('admin.image-generation.save-failed'))),
    })

  const number = (name: 'maxPerCall' | 'maxPerRun' | 'daily') => (
    <FormField
      control={form.control}
      name={name}
      render={({ field }) => (
        <FormItem>
          <FormLabel>{t(`admin.image-generation.${name}`)}</FormLabel>
          <FormControl>
            <Input type="number" inputMode="numeric" className="w-32" {...field} value={field.value as number | string} />
          </FormControl>
          <FormDescription>{t(`admin.image-generation.${name}-hint`)}</FormDescription>
          <FormMessage />
        </FormItem>
      )}
    />
  )

  const quality = (name: 'defaultQuality' | 'maxQuality') => (
    <FormField
      control={form.control}
      name={name}
      render={({ field }) => (
        <FormItem>
          <FormLabel>{t(name === 'defaultQuality' && qualityFixed ? 'admin.image-generation.fixedQuality' : `admin.image-generation.${name}`)}</FormLabel>
          <Select value={field.value} onValueChange={field.onChange}>
            <FormControl>
              <SelectTrigger className="w-48">
                <SelectValue />
              </SelectTrigger>
            </FormControl>
            <SelectContent>
              {QUALITIES.map((level) => (
                <SelectItem key={level} value={level}>
                  {level === 'auto' ? t(`admin.image-generation.quality-auto-${name}`) : t(`admin.image-generation.quality-${level}`)}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          <FormDescription>
            {t(name === 'defaultQuality' && qualityFixed ? 'admin.image-generation.fixedQuality-hint' : `admin.image-generation.${name}-hint`)}
          </FormDescription>
          <FormMessage />
        </FormItem>
      )}
    />
  )

  const list = models.data ?? []
  return (
    <Form {...form}>
      <form className="space-y-10" onSubmit={form.handleSubmit(save)}>
        <Section title={t('admin.image-generation.model-section')} description={t('admin.image-generation.model-section-hint')}>
          <FormField
            control={form.control}
            name="model"
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('admin.image-generation.model')}</FormLabel>
                <Select value={field.value || undefined} onValueChange={field.onChange} disabled={models.isPending}>
                  <FormControl>
                    <SelectTrigger className="w-full max-w-md">
                      <SelectValue placeholder={t(list.length ? 'admin.image-generation.model-placeholder' : 'admin.image-generation.model-none')} />
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
                  {t('admin.image-generation.model-hint')}{' '}
                  <Link to="/admin/models" className="underline underline-offset-4">
                    {t('admin.image-generation.model-hint-link')}
                  </Link>
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />
          {chosen && chosen === settings.model && !settings.modelPriced && (
            <Alert>
              <TriangleAlert aria-hidden="true" />
              <AlertTitle>{t('admin.image-generation.unpriced')}</AlertTitle>
              <AlertDescription>{t('admin.image-generation.unpriced-hint')}</AlertDescription>
            </Alert>
          )}
        </Section>

        <Section title={t('admin.image-generation.quality')} description={t('admin.image-generation.quality-hint')}>
          <FormField
            control={form.control}
            name="qualityFixed"
            render={({ field }) => (
              <FormItem className="flex flex-row items-center justify-between gap-4 rounded-lg border p-4">
                <div className="space-y-0.5">
                  <FormLabel className="text-base">{t('admin.image-generation.qualityFixed')}</FormLabel>
                  <FormDescription>{t('admin.image-generation.qualityFixed-hint')}</FormDescription>
                </div>
                <FormControl>
                  <Switch checked={field.value} onCheckedChange={field.onChange} />
                </FormControl>
              </FormItem>
            )}
          />
          <div className="grid gap-6 sm:grid-cols-2">
            {quality('defaultQuality')}
            {!qualityFixed && quality('maxQuality')}
          </div>
        </Section>

        <Section title={t('admin.image-generation.complexity')} description={t('admin.image-generation.complexity-hint')}>
          <FormField
            control={form.control}
            name="complexityFixed"
            render={({ field }) => (
              <FormItem className="flex flex-row items-center justify-between gap-4 rounded-lg border p-4">
                <div className="space-y-0.5">
                  <FormLabel className="text-base">{t('admin.image-generation.complexityFixed')}</FormLabel>
                  <FormDescription>{t('admin.image-generation.complexityFixed-hint')}</FormDescription>
                </div>
                <FormControl>
                  <Switch checked={field.value} onCheckedChange={field.onChange} />
                </FormControl>
              </FormItem>
            )}
          />
          <FormField
            control={form.control}
            name="defaultComplexity"
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t(complexityFixed ? 'admin.image-generation.fixedComplexity' : 'admin.image-generation.defaultComplexity')}</FormLabel>
                <Select value={field.value} onValueChange={field.onChange}>
                  <FormControl>
                    <SelectTrigger className="w-48">
                      <SelectValue />
                    </SelectTrigger>
                  </FormControl>
                  <SelectContent>
                    {COMPLEXITIES.map((level) => (
                      <SelectItem key={level} value={level}>
                        {t(`admin.image-generation.complexity-${level}`)}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
                <FormDescription>
                  {t(complexityFixed ? 'admin.image-generation.fixedComplexity-hint' : 'admin.image-generation.defaultComplexity-hint')}
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />
        </Section>

        <Section title={t('admin.image-generation.limits')} description={t('admin.image-generation.limits-hint')}>
          <div className="grid gap-6 sm:grid-cols-3">
            {number('maxPerCall')}
            {number('maxPerRun')}
            {number('daily')}
          </div>
        </Section>

        <Button type="submit" disabled={update.isPending || !form.formState.isDirty}>
          {t('admin.image-generation.save')}
        </Button>
      </form>
    </Form>
  )
}

function TryPicture({ disabled }: { disabled: boolean }) {
  const { t } = useTranslation()
  const [prompt, setPrompt] = useState('')
  const test = useMutation({
    mutationFn: async (text: string) => unwrap(await api.POST('/admin/image-generation/test', { body: { prompt: text } })),
  })
  return (
    <div className="space-y-2">
      <form
        className="flex gap-2"
        onSubmit={(event) => {
          event.preventDefault()
          if (prompt.trim()) test.mutate(prompt.trim())
        }}
      >
        <Input
          className="flex-1"
          placeholder={t('admin.image-generation.test-placeholder')}
          aria-label={t('admin.image-generation.test')}
          value={prompt}
          onChange={(event) => setPrompt(event.target.value)}
        />
        <Button type="submit" variant="outline" disabled={disabled || test.isPending || !prompt.trim()}>
          {test.isPending && <Loader2 className="animate-spin" aria-hidden="true" />}
          {t('admin.image-generation.test-run')}
        </Button>
      </form>
      {disabled && <p className="text-muted-foreground text-sm">{t('admin.image-generation.test-no-model')}</p>}
      {test.isError && (
        <Alert variant="destructive">
          <XCircle aria-hidden="true" />
          <AlertTitle>{t('admin.image-generation.test-failed')}</AlertTitle>
        </Alert>
      )}
      {test.data &&
        (test.data.ok && test.data.image ? (
          <Alert>
            <CheckCircle2 aria-hidden="true" />
            <AlertTitle>
              {t('admin.image-generation.test-ok', { seconds: (test.data.durationMs / 1000).toFixed(1) })}
              {test.data.width && test.data.height ? ` · ${test.data.width} × ${test.data.height}` : ''}
              {test.data.cost != null ? ` · $${test.data.cost.toFixed(4)}` : ''}
            </AlertTitle>
            <AlertDescription>
              <img src={test.data.image} alt={prompt} className="mt-2 max-h-64 rounded-md" />
            </AlertDescription>
          </Alert>
        ) : (
          <Alert variant="destructive">
            <XCircle aria-hidden="true" />
            <AlertTitle>{t('admin.image-generation.test-failed')}</AlertTitle>
            <AlertDescription>{test.data.error}</AlertDescription>
          </Alert>
        ))}
    </div>
  )
}
