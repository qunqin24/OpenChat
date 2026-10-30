import { zodResolver } from '@hookform/resolvers/zod'
import { OpenChatError } from '@openchat/api-client'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { z } from 'zod'

import { ConfigDrawer } from '@/components/shadcn/config-drawer'
import { Header } from '@/components/shadcn/layout/header'
import { Main } from '@/components/shadcn/layout/main'
import { ProfileDropdown } from '@/components/shadcn/profile-dropdown'
import { Search } from '@/components/shadcn/search'
import { Badge } from '@/components/shadcn/ui/badge'
import { Button } from '@/components/shadcn/ui/button'
import { Form, FormControl, FormDescription, FormField, FormItem, FormLabel, FormMessage } from '@/components/shadcn/ui/form'
import { Input } from '@/components/shadcn/ui/input'
import { Label } from '@/components/shadcn/ui/label'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/shadcn/ui/select'
import { Separator } from '@/components/shadcn/ui/separator'
import { Skeleton } from '@/components/shadcn/ui/skeleton'
import { Switch } from '@/components/shadcn/ui/switch'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/shadcn/ui/tabs'
import { Textarea } from '@/components/shadcn/ui/textarea'
import { usePasskeySettings, useUpdatePasskeySettings, type PasskeySettings } from './use-passkey-settings'

/**
 * Authentication: how accounts may prove who they are, one tab per method.
 * Passkeys are the first; the page is laid out for more (single sign-on, a
 * second factor) to join them as tabs of their own.
 */
export function Authentication() {
  const { t } = useTranslation()
  return (
    <>
      <Header fixed>
        <Search className="me-auto" />
        <ConfigDrawer />
        <ProfileDropdown />
      </Header>

      <Main className="flex flex-1 flex-col">
        <div>
          <h2 className="text-2xl font-bold tracking-tight">{t('admin.authentication.title')}</h2>
          <p className="text-muted-foreground">{t('admin.authentication.subtitle')}</p>
        </div>
        <Separator className="my-4 lg:my-6" />
        <Tabs defaultValue="passkeys" className="max-w-2xl">
          <TabsList>
            <TabsTrigger value="passkeys">{t('admin.authentication.passkeys.tab')}</TabsTrigger>
          </TabsList>
          <TabsContent value="passkeys" className="pt-4">
            <PasskeysTab />
          </TabsContent>
        </Tabs>
      </Main>
    </>
  )
}

export default Authentication

function PasskeysTab() {
  const { data, isPending } = usePasskeySettings()
  if (isPending || !data) {
    return (
      <div className="space-y-4">
        <Skeleton className="h-20 w-full" />
        <Skeleton className="h-60 w-full" />
      </div>
    )
  }
  return (
    <div className="space-y-10 pb-12">
      <EnabledSwitch settings={data} />
      <RelyingPartyForm key={`${data.rpId}|${data.origins.join(',')}|${data.userVerification}`} settings={data} />
      <AndroidAppsForm key={androidLines(data).join('|')} settings={data} />
    </div>
  )
}

/** On or off, saved the moment it moves, with how many passkeys there are. */
function EnabledSwitch({ settings }: { settings: PasskeySettings }) {
  const { t } = useTranslation()
  const update = useUpdatePasskeySettings()
  return (
    <div className="flex flex-row items-center justify-between gap-6 rounded-lg border p-4">
      <div className="space-y-0.5">
        <div className="flex items-center gap-2">
          <Label htmlFor="passkeys-enabled" className="text-base">
            {t('admin.authentication.passkeys.enabled')}
          </Label>
          <Badge variant={settings.enabled ? 'default' : 'secondary'}>
            {settings.enabled ? t('admin.authentication.passkeys.on') : t('admin.authentication.passkeys.off')}
          </Badge>
        </div>
        <p className="text-muted-foreground text-sm">
          {t(settings.enabled ? 'admin.authentication.passkeys.on-hint' : 'admin.authentication.passkeys.off-hint', {
            count: settings.registered,
          })}
        </p>
      </div>
      <Switch
        id="passkeys-enabled"
        checked={settings.enabled}
        disabled={update.isPending}
        onCheckedChange={(enabled) =>
          update.mutate(
            { enabled },
            {
              onSuccess: () => toast.success(t(enabled ? 'admin.authentication.passkeys.turned-on' : 'admin.authentication.passkeys.turned-off')),
              onError: () => toast.error(t('admin.authentication.save-failed')),
            },
          )
        }
      />
    </div>
  )
}

/** Where passkeys are bound and how strictly the person is checked: one form, one save. */
function RelyingPartyForm({ settings }: { settings: PasskeySettings }) {
  const { t } = useTranslation()
  const update = useUpdatePasskeySettings()

  const schema = z.object({
    rpId: z
      .string()
      .trim()
      .refine((value) => value === '' || /^[a-z0-9.-]+$/i.test(value), { message: t('admin.authentication.passkeys.rp-id-invalid') }),
    origins: z.string().refine(
      (value) =>
        value
          .split('\n')
          .map((line) => line.trim())
          .filter(Boolean)
          .every((line) => /^https?:\/\/[^/\s]+$/.test(line.replace(/\/$/, ''))),
      { message: t('admin.authentication.passkeys.origins-invalid') },
    ),
    userVerification: z.enum(['required', 'preferred']),
  })
  type Values = z.infer<typeof schema>

  const form = useForm<Values>({
    resolver: zodResolver(schema),
    defaultValues: { rpId: settings.rpId, origins: settings.origins.join('\n'), userVerification: settings.userVerification },
  })

  const submit = (values: Values) =>
    update.mutate(
      {
        rpId: values.rpId.trim(),
        origins: values.origins
          .split('\n')
          .map((line) => line.trim())
          .filter(Boolean),
        userVerification: values.userVerification,
      },
      {
        onSuccess: () => toast.success(t('admin.authentication.saved')),
        onError: (error) => {
          if (error instanceof OpenChatError && error.code === 'invalid_rp_id') {
            form.setError('rpId', { message: t('admin.authentication.passkeys.rp-id-invalid') })
            return
          }
          if (error instanceof OpenChatError && error.code === 'invalid_origin') {
            form.setError('origins', { message: t('admin.authentication.passkeys.origins-invalid') })
            return
          }
          toast.error(t('admin.authentication.save-failed'))
        },
      },
    )

  return (
    <Form {...form}>
      <form className="space-y-8" onSubmit={form.handleSubmit(submit)}>
        <div>
          <h3 className="text-lg font-medium">{t('admin.authentication.passkeys.rp')}</h3>
          <p className="text-muted-foreground text-sm">{t('admin.authentication.passkeys.rp-hint')}</p>
        </div>
        <FormField
          control={form.control}
          name="rpId"
          render={({ field }) => (
            <FormItem>
              <FormLabel>{t('admin.authentication.passkeys.rp-id')}</FormLabel>
              <FormControl>
                <Input {...field} placeholder={settings.effectiveRpId} autoComplete="off" spellCheck={false} />
              </FormControl>
              <FormDescription>{t('admin.authentication.passkeys.rp-id-hint', { current: settings.effectiveRpId })}</FormDescription>
              <FormMessage />
            </FormItem>
          )}
        />
        <FormField
          control={form.control}
          name="origins"
          render={({ field }) => (
            <FormItem>
              <FormLabel>{t('admin.authentication.passkeys.origins')}</FormLabel>
              <FormControl>
                <Textarea {...field} rows={3} placeholder={settings.effectiveOrigins.join('\n')} spellCheck={false} className="font-mono text-sm" />
              </FormControl>
              <FormDescription>
                {t('admin.authentication.passkeys.origins-hint', { current: settings.effectiveOrigins.join(', ') })}
              </FormDescription>
              <FormMessage />
            </FormItem>
          )}
        />
        <FormField
          control={form.control}
          name="userVerification"
          render={({ field }) => (
            <FormItem>
              <FormLabel>{t('admin.authentication.passkeys.verification')}</FormLabel>
              <Select value={field.value} onValueChange={field.onChange}>
                <FormControl>
                  <SelectTrigger className="w-[240px]">
                    <SelectValue />
                  </SelectTrigger>
                </FormControl>
                <SelectContent>
                  <SelectItem value="preferred">{t('admin.authentication.passkeys.verification-preferred')}</SelectItem>
                  <SelectItem value="required">{t('admin.authentication.passkeys.verification-required')}</SelectItem>
                </SelectContent>
              </Select>
              <FormDescription>{t('admin.authentication.passkeys.verification-hint')}</FormDescription>
            </FormItem>
          )}
        />
        <Button type="submit" disabled={update.isPending || !form.formState.isDirty}>
          {t('admin.authentication.save')}
        </Button>
      </form>
    </Form>
  )
}

/** The apps as the form writes them: one line per signing key, "package fingerprint". */
function androidLines(settings: PasskeySettings): string[] {
  return settings.androidApps.flatMap((app) => app.fingerprints.map((fingerprint) => `${app.packageName} ${fingerprint}`))
}

const PACKAGE_NAME = /^[a-zA-Z][a-zA-Z0-9_]*(\.[a-zA-Z][a-zA-Z0-9_]*)+$/
const FINGERPRINT = /^([0-9a-fA-F]{2}:?){31}[0-9a-fA-F]{2}$/

/**
 * The Android app's passkeys: the apps (package name and the SHA-256 of each
 * certificate they are signed with) that may sign in with this site's
 * passkeys. One line per key, so the release build and a debug build of the
 * same or another package each get a line; they go into assetlinks.json and
 * the origins an app's answer may carry. Android checks assetlinks.json on
 * the RP ID's host, hence the hint to pin it.
 */
function AndroidAppsForm({ settings }: { settings: PasskeySettings }) {
  const { t } = useTranslation()
  const update = useUpdatePasskeySettings()

  const schema = z.object({
    apps: z.string().refine(
      (value) =>
        value
          .split('\n')
          .map((line) => line.trim())
          .filter(Boolean)
          .every((line) => {
            const [name, fingerprint, ...rest] = line.split(/\s+/)
            return rest.length === 0 && PACKAGE_NAME.test(name ?? '') && FINGERPRINT.test(fingerprint ?? '')
          }),
      { message: t('admin.authentication.passkeys.android-invalid') },
    ),
  })
  type Values = z.infer<typeof schema>

  const form = useForm<Values>({ resolver: zodResolver(schema), defaultValues: { apps: androidLines(settings).join('\n') } })

  const submit = (values: Values) => {
    const byPackage = new Map<string, string[]>()
    for (const line of values.apps.split('\n').map((l) => l.trim()).filter(Boolean)) {
      const [name, fingerprint] = line.split(/\s+/) as [string, string]
      byPackage.set(name, [...(byPackage.get(name) ?? []), fingerprint])
    }
    update.mutate(
      { androidApps: [...byPackage].map(([packageName, fingerprints]) => ({ packageName, fingerprints })) },
      {
        onSuccess: () => toast.success(t('admin.authentication.saved')),
        onError: (error) => {
          if (error instanceof OpenChatError && (error.code === 'invalid_package_name' || error.code === 'invalid_fingerprint')) {
            form.setError('apps', { message: t('admin.authentication.passkeys.android-invalid') })
            return
          }
          toast.error(t('admin.authentication.save-failed'))
        },
      },
    )
  }

  return (
    <Form {...form}>
      <form className="space-y-6" onSubmit={form.handleSubmit(submit)}>
        <div>
          <h3 className="text-lg font-medium">{t('admin.authentication.passkeys.android')}</h3>
          <p className="text-muted-foreground text-sm">{t('admin.authentication.passkeys.android-hint')}</p>
        </div>
        <FormField
          control={form.control}
          name="apps"
          render={({ field }) => (
            <FormItem>
              <FormLabel>{t('admin.authentication.passkeys.android-apps')}</FormLabel>
              <FormControl>
                <Textarea {...field} rows={3} placeholder="org.qunqin.openchat AB:CD:…" spellCheck={false} className="font-mono text-sm" />
              </FormControl>
              <FormDescription>
                {settings.rpId
                  ? t('admin.authentication.passkeys.android-apps-hint')
                  : t('admin.authentication.passkeys.android-pin-rp', { current: settings.effectiveRpId })}
              </FormDescription>
              <FormMessage />
            </FormItem>
          )}
        />
        {settings.androidOrigins.length > 0 && (
          <div className="space-y-1">
            <Label>{t('admin.authentication.passkeys.android-origins')}</Label>
            <ul className="text-muted-foreground font-mono text-xs break-all">
              {settings.androidOrigins.map((origin) => (
                <li key={origin}>{origin}</li>
              ))}
            </ul>
          </div>
        )}
        <Button type="submit" disabled={update.isPending || !form.formState.isDirty}>
          {t('admin.authentication.save')}
        </Button>
      </form>
    </Form>
  )
}
