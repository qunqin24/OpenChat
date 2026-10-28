import { OpenChatError } from '@openchat/api-client'
import { zodResolver } from '@hookform/resolvers/zod'
import { CheckCircle2, Info, Loader2, XCircle } from 'lucide-react'
import { useState, type ReactNode } from 'react'
import { useForm, useWatch } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { z } from 'zod'

import { ConfigDrawer } from '@/components/shadcn/config-drawer'
import { Header } from '@/components/shadcn/layout/header'
import { Main } from '@/components/shadcn/layout/main'
import { PasswordInput } from '@/components/shadcn/password-input'
import { ProfileDropdown } from '@/components/shadcn/profile-dropdown'
import { Search } from '@/components/shadcn/search'
import { Alert, AlertDescription, AlertTitle } from '@/components/shadcn/ui/alert'
import { Button } from '@/components/shadcn/ui/button'
import {
  Form,
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/shadcn/ui/form'
import { Input } from '@/components/shadcn/ui/input'
import { RadioGroup, RadioGroupItem } from '@/components/shadcn/ui/radio-group'
import { Separator } from '@/components/shadcn/ui/separator'
import { QuotaForm } from './quota-form'
import { Skeleton } from '@/components/shadcn/ui/skeleton'
import { Switch } from '@/components/shadcn/ui/switch'
import {
  useStorageSettings,
  useTestStorage,
  useUpdateStorageSettings,
  type StorageSettings,
  type StorageTestResult,
  type UpdateStorageSettings,
} from './use-storage'

/** An http(s) URL with a host, as the server reads one. */
function validHttpUrl(raw: string): boolean {
  try {
    const url = new URL(raw)
    return (url.protocol === 'http:' || url.protocol === 'https:') && url.hostname !== ''
  } catch {
    return false
  }
}

/** An http(s) origin: a URL with nothing after the host. */
function validOrigin(raw: string): boolean {
  if (!validHttpUrl(raw)) return false
  const url = new URL(raw)
  return (
    (url.pathname === '' || url.pathname === '/') &&
    url.search === '' &&
    url.hash === '' &&
    url.username === '' &&
    url.password === ''
  )
}

/**
 * File storage: where attachments are kept. Its own page, with the settings of
 * both drivers behind one choice, because it is the one place the server reads
 * its storage configuration from — there are no environment variables for it.
 *
 * One form, one save. Saving applies the settings to the running server (it
 * builds the new storage first and keeps the old if that fails, so a wrong
 * path is an error here and not an outage), and "test" tries the values on the
 * form without saving them. The secret access key is write-only: a blank field
 * leaves the stored one alone.
 */
export function FileStorage() {
  const { t } = useTranslation()
  const { data, isPending } = useStorageSettings()

  return (
    <>
      <Header fixed>
        <Search className="me-auto" />
        <ConfigDrawer />
        <ProfileDropdown />
      </Header>

      <Main className="flex flex-1 flex-col">
        <div>
          <h2 className="text-2xl font-bold tracking-tight">{t('admin.storage.title')}</h2>
          <p className="text-muted-foreground">{t('admin.storage.subtitle')}</p>
        </div>
        <Separator className="my-4 lg:my-6" />

        {isPending || !data ? (
          <div className="max-w-2xl space-y-4">
            <Skeleton className="h-40 w-full" />
            <Skeleton className="h-72 w-full" />
          </div>
        ) : (
          <div className="max-w-2xl space-y-10 pb-12">
            <QuotaForm />
            <Separator />
            <StorageForm settings={data} />
          </div>
        )}
      </Main>
    </>
  )
}

export default FileStorage

function Section({
  title,
  description,
  children,
}: {
  title: string
  description: string
  children: ReactNode
}) {
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

function StorageForm({ settings }: { settings: StorageSettings }) {
  const { t } = useTranslation()
  const update = useUpdateStorageSettings()
  const test = useTestStorage()
  const [saveError, setSaveError] = useState<string | null>(null)

  const required = t('admin.storage.required')
  const schema = z
    .object({
      driver: z.enum(['local', 's3']),
      localDir: z.string().trim().min(1, required),
      publicUrl: z
        .string()
        .trim()
        .refine((value) => value === '' || validOrigin(value), { message: t('admin.storage.public-url-invalid') }),
      s3: z.object({
        endpoint: z
          .string()
          .trim()
          .refine((value) => value === '' || validHttpUrl(value), { message: t('admin.storage.endpoint-invalid') }),
        region: z.string().trim(),
        bucket: z.string().trim(),
        accessKeyId: z.string().trim(),
        // Blank leaves the stored secret as it is.
        secretAccessKey: z.string().trim(),
        pathStyle: z.boolean(),
      }),
    })
    .superRefine((value, ctx) => {
      // The bucket only matters to the driver that uses it.
      if (value.driver === 's3' && value.s3.bucket === '') {
        ctx.addIssue({ code: 'custom', path: ['s3', 'bucket'], message: required })
      }
    })
  type Values = z.infer<typeof schema>

  const valuesOf = (s: StorageSettings): Values => ({
    driver: s.driver,
    localDir: s.localDir,
    publicUrl: s.publicUrl,
    s3: {
      endpoint: s.s3.endpoint,
      region: s.s3.region,
      bucket: s.s3.bucket,
      accessKeyId: s.s3.accessKeyId,
      secretAccessKey: '',
      pathStyle: s.s3.pathStyle,
    },
  })
  const form = useForm<Values>({ resolver: zodResolver(schema), defaultValues: valuesOf(settings) })
  const driver = useWatch({ control: form.control, name: 'driver' })
  const localDir = useWatch({ control: form.control, name: 'localDir' })

  /** The form as a request; the same body saves it and tests it. */
  const bodyOf = (values: Values): UpdateStorageSettings => ({
    driver: values.driver,
    localDir: values.localDir,
    publicUrl: values.publicUrl,
    s3: {
      endpoint: values.s3.endpoint,
      region: values.s3.region,
      bucket: values.s3.bucket,
      accessKeyId: values.s3.accessKeyId,
      pathStyle: values.s3.pathStyle,
      ...(values.s3.secretAccessKey ? { secretAccessKey: values.s3.secretAccessKey } : {}),
    },
  })

  const submit = (values: Values) => {
    setSaveError(null)
    update.mutate(bodyOf(values), {
      onSuccess: (next) => {
        form.reset(valuesOf(next))
        test.reset()
        toast.success(t('admin.storage.saved'))
      },
      onError: (error) => {
        if (error instanceof OpenChatError) {
          // The server names the input only when one is to blame; the words
          // shown are ours, in this language.
          const fieldMessages: Record<string, string> = {
            invalid_local_dir: t('admin.storage.required'),
            invalid_public_url: t('admin.storage.public-url-invalid'),
            invalid_endpoint: t('admin.storage.endpoint-invalid'),
            bucket_required: t('admin.storage.required'),
          }
          const message = fieldMessages[error.code]
          if (message && error.field) {
            form.setError(error.field as 'localDir' | 'publicUrl' | 's3.endpoint' | 's3.bucket', { message })
            return
          }
          // The storage could not be set up: the server's own words (a path, an
          // operating system's reason) are what an administrator needs to fix it.
          if (error.code === 'storage_unavailable') {
            setSaveError(error.message)
            return
          }
        }
        toast.error(t('admin.storage.save-failed'))
      },
    })
  }

  const runTest = async () => {
    if (!(await form.trigger())) return
    test.mutate(bodyOf(form.getValues()))
  }

  const localDirHint =
    localDir.trim() === settings.localDir
      ? t('admin.storage.local-dir-resolved', { path: settings.resolvedLocalDir })
      : t('admin.storage.local-dir-relative')

  return (
    <Form {...form}>
      <form className="space-y-10" onSubmit={form.handleSubmit(submit)}>
        <Section title={t('admin.storage.driver')} description={t('admin.storage.driver-hint')}>
          <FormField
            control={form.control}
            name="driver"
            render={({ field }) => (
              <FormItem className="space-y-3">
                <FormControl>
                  <RadioGroup
                    value={field.value}
                    onValueChange={(value) => {
                      field.onChange(value)
                      test.reset()
                    }}
                    className="gap-3"
                  >
                    <RadioOption value="local" label={t('admin.storage.driver-local')} hint={t('admin.storage.driver-local-hint')} />
                    <RadioOption value="s3" label={t('admin.storage.driver-s3')} hint={t('admin.storage.driver-s3-hint')} />
                  </RadioGroup>
                </FormControl>
                <FormDescription>{t('admin.storage.switch-note')}</FormDescription>
              </FormItem>
            )}
          />
        </Section>

        {driver === 'local' ? (
          <Section title={t('admin.storage.local')} description={t('admin.storage.local-hint')}>
            <div className="space-y-8">
              <FormField
                control={form.control}
                name="localDir"
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('admin.storage.local-dir')}</FormLabel>
                    <FormControl>
                      <Input spellCheck={false} className="font-mono text-sm" placeholder="./data" {...field} />
                    </FormControl>
                    <FormDescription className="break-all">{localDirHint}</FormDescription>
                    <FormMessage />
                  </FormItem>
                )}
              />
              <FormField
                control={form.control}
                name="publicUrl"
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('admin.storage.public-url')}</FormLabel>
                    <FormControl>
                      <Input spellCheck={false} placeholder="https://chat.example.com" {...field} />
                    </FormControl>
                    <FormDescription>{t('admin.storage.public-url-hint')}</FormDescription>
                    <FormMessage />
                  </FormItem>
                )}
              />
              <Alert>
                <Info aria-hidden="true" />
                <AlertDescription>{t('admin.storage.dir-note')}</AlertDescription>
              </Alert>
            </div>
          </Section>
        ) : (
          <Section title={t('admin.storage.s3')} description={t('admin.storage.s3-hint')}>
            <div className="space-y-8">
              <FormField
                control={form.control}
                name="s3.endpoint"
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('admin.storage.endpoint')}</FormLabel>
                    <FormControl>
                      <Input spellCheck={false} placeholder="http://localhost:9000" {...field} />
                    </FormControl>
                    <FormDescription>{t('admin.storage.endpoint-hint')}</FormDescription>
                    <FormMessage />
                  </FormItem>
                )}
              />
              <div className="grid gap-8 sm:grid-cols-2">
                <FormField
                  control={form.control}
                  name="s3.bucket"
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>{t('admin.storage.bucket')}</FormLabel>
                      <FormControl>
                        <Input spellCheck={false} placeholder="openchat" {...field} />
                      </FormControl>
                      <FormMessage />
                    </FormItem>
                  )}
                />
                <FormField
                  control={form.control}
                  name="s3.region"
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>{t('admin.storage.region')}</FormLabel>
                      <FormControl>
                        <Input spellCheck={false} placeholder="us-east-1" {...field} />
                      </FormControl>
                      <FormMessage />
                    </FormItem>
                  )}
                />
              </div>
              <FormField
                control={form.control}
                name="s3.accessKeyId"
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('admin.storage.access-key-id')}</FormLabel>
                    <FormControl>
                      <Input spellCheck={false} autoComplete="off" {...field} />
                    </FormControl>
                    <FormDescription>{t('admin.storage.credentials-hint')}</FormDescription>
                    <FormMessage />
                  </FormItem>
                )}
              />
              <FormField
                control={form.control}
                name="s3.secretAccessKey"
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('admin.storage.secret-access-key')}</FormLabel>
                    <div className="flex gap-2">
                      <FormControl>
                        <PasswordInput
                          className="flex-1"
                          autoComplete="new-password"
                          placeholder={
                            settings.s3.secretKeySet
                              ? t('admin.storage.secret-set', { preview: settings.s3.secretKeyPreview ?? '' })
                              : ''
                          }
                          {...field}
                        />
                      </FormControl>
                      {settings.s3.secretKeySet && (
                        <Button
                          type="button"
                          variant="outline"
                          disabled={update.isPending}
                          onClick={() =>
                            update.mutate(
                              { s3: { secretAccessKey: '' } },
                              {
                                onSuccess: () => {
                                  test.reset()
                                  toast.success(t('admin.storage.secret-removed'))
                                },
                                onError: () => toast.error(t('admin.storage.save-failed')),
                              },
                            )
                          }
                        >
                          {t('admin.channels.api-key-clear')}
                        </Button>
                      )}
                    </div>
                    <FormDescription>
                      {settings.s3.secretKeySet ? t('admin.storage.secret-stored') : t('admin.storage.secret-missing')}
                    </FormDescription>
                    <FormMessage />
                  </FormItem>
                )}
              />
              <FormField
                control={form.control}
                name="s3.pathStyle"
                render={({ field }) => (
                  <FormItem className="flex flex-row items-center justify-between gap-6 rounded-lg border p-4">
                    <div className="space-y-0.5">
                      <FormLabel>{t('admin.storage.path-style')}</FormLabel>
                      <FormDescription>{t('admin.storage.path-style-hint')}</FormDescription>
                    </div>
                    <FormControl>
                      <Switch checked={field.value} onCheckedChange={field.onChange} />
                    </FormControl>
                  </FormItem>
                )}
              />
              <Alert>
                <Info aria-hidden="true" />
                <AlertTitle>{t('admin.storage.cors-title')}</AlertTitle>
                <AlertDescription>{t('admin.storage.cors-note')}</AlertDescription>
              </Alert>
            </div>
          </Section>
        )}

        <div className="space-y-4">
          <div className="flex flex-wrap gap-2">
            <Button type="submit" disabled={!form.formState.isDirty || update.isPending}>
              {update.isPending && <Loader2 className="animate-spin" aria-hidden="true" />}
              {t('admin.storage.save')}
            </Button>
            <Button type="button" variant="outline" disabled={test.isPending} onClick={runTest}>
              {test.isPending && <Loader2 className="animate-spin" aria-hidden="true" />}
              {t('admin.storage.test-run')}
            </Button>
            {form.formState.isDirty && (
              <Button
                type="button"
                variant="ghost"
                disabled={update.isPending}
                onClick={() => {
                  form.reset(valuesOf(settings))
                  setSaveError(null)
                  test.reset()
                }}
              >
                {t('admin.ui.cancel')}
              </Button>
            )}
          </div>
          <p className="text-muted-foreground text-sm">{t('admin.storage.test-hint')}</p>
          {saveError && (
            <Alert variant="destructive">
              <XCircle aria-hidden="true" />
              <AlertTitle>{t('admin.storage.unavailable')}</AlertTitle>
              <AlertDescription>{saveError}</AlertDescription>
            </Alert>
          )}
          {test.data && <TestOutcome result={test.data} />}
          {test.isError && (
            <Alert variant="destructive">
              <XCircle aria-hidden="true" />
              <AlertTitle>{t('admin.storage.save-failed')}</AlertTitle>
            </Alert>
          )}
        </div>
      </form>
    </Form>
  )
}

function TestOutcome({ result }: { result: StorageTestResult }) {
  const { t } = useTranslation()
  if (!result.ok) {
    return (
      <Alert variant="destructive">
        <XCircle aria-hidden="true" />
        <AlertTitle>{t('admin.storage.test-failed')}</AlertTitle>
        {/* The storage's own words: for an administrator chasing a bad path or
            a denied key, the exact reason is the useful part. */}
        {result.error && <AlertDescription>{result.error}</AlertDescription>}
      </Alert>
    )
  }
  return (
    <Alert>
      <CheckCircle2 aria-hidden="true" />
      <AlertTitle>
        {t('admin.storage.test-ok')} ·{' '}
        <span className="text-muted-foreground font-normal tabular-nums">
          {t('admin.storage.test-summary', { ms: result.latencyMs })}
        </span>
      </AlertTitle>
      <AlertDescription>{t('admin.storage.test-ok-hint')}</AlertDescription>
    </Alert>
  )
}

/** One choice of a radio list, with a line saying what choosing it means. */
function RadioOption({ value, label, hint }: { value: string; label: string; hint: string }) {
  return (
    <FormItem className="flex items-start gap-3">
      <FormControl>
        <RadioGroupItem value={value} className="mt-0.5" />
      </FormControl>
      <div className="space-y-1 leading-none">
        <FormLabel className="font-normal">{label}</FormLabel>
        <FormDescription>{hint}</FormDescription>
      </div>
    </FormItem>
  )
}
