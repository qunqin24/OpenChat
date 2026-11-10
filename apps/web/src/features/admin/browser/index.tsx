import { isOpenChatError, type components } from '@openchat/api-client'
import { zodResolver } from '@hookform/resolvers/zod'
import { keepPreviousData, useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { CheckCircle2, Loader2, XCircle } from 'lucide-react'
import { useCallback, useState, type ReactNode } from 'react'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { z } from 'zod'

import { ConfigDrawer } from '@/components/shadcn/config-drawer'
import { DataTableCursorPagination } from '@/components/shadcn/data-table'
import { Header } from '@/components/shadcn/layout/header'
import { Main } from '@/components/shadcn/layout/main'
import { LongText } from '@/components/shadcn/long-text'
import { Search } from '@/components/shadcn/search'
import { Alert, AlertDescription, AlertTitle } from '@/components/shadcn/ui/alert'
import { Badge } from '@/components/shadcn/ui/badge'
import { Button } from '@/components/shadcn/ui/button'
import { Form, FormControl, FormDescription, FormField, FormItem, FormLabel, FormMessage } from '@/components/shadcn/ui/form'
import { Input } from '@/components/shadcn/ui/input'
import { Label } from '@/components/shadcn/ui/label'
import { Separator } from '@/components/shadcn/ui/separator'
import { Skeleton } from '@/components/shadcn/ui/skeleton'
import { Switch } from '@/components/shadcn/ui/switch'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/shadcn/ui/table'
import { Textarea } from '@/components/shadcn/ui/textarea'
import { adminKeys } from '@/features/admin/use-admin'
import { formatLogTime } from '@/features/admin/usage-logs/format'
import { api, unwrap } from '@/lib/api'
import { queryKeys } from '@openchat/chat-core/query-keys'

type BrowserSettings = components['schemas']['BrowserSettings']
type UpdateBrowserSettings = components['schemas']['UpdateBrowserSettingsRequest']

function useBrowserSettings() {
  return useQuery({
    queryKey: adminKeys.browser(),
    queryFn: async ({ signal }) => unwrap(await api.GET('/admin/browser', { signal })),
  })
}

function useUpdateBrowserSettings() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async (body: UpdateBrowserSettings) => unwrap(await api.PATCH('/admin/browser', { body })),
    onSuccess: (next) => {
      queryClient.setQueryData(adminKeys.browser(), next)
      // The composer's "+" offers the browser with these settings.
      void queryClient.invalidateQueries({ queryKey: queryKeys.features })
    },
  })
}

/** What a failed save says: the server's reason for settings it refused. */
function saveError(error: unknown, fallback: string) {
  return isOpenChatError(error) && error.code === 'invalid_browser_settings' ? error.message : fallback
}

/**
 * The browser tool (api/browser.go, docs/design/browser-plan.md): on or off -
 * administrators only, for now - where the headless Chromium is, the proxy
 * every page goes through, a page to try, and the actions that needed asking.
 */
export function BrowserSettingsPage() {
  const { t } = useTranslation()
  const { data, isPending } = useBrowserSettings()

  return (
    <>
      <Header fixed>
        <Search className="me-auto" />
        <ConfigDrawer />
      </Header>

      <Main className="flex flex-1 flex-col">
        <div>
          <h2 className="text-2xl font-bold tracking-tight">{t('admin.browser.title')}</h2>
          <p className="text-muted-foreground">{t('admin.browser.subtitle')}</p>
        </div>
        <Separator className="my-4 lg:my-6" />

        {isPending || !data ? (
          <div className="max-w-2xl space-y-4">
            <Skeleton className="h-20 w-full" />
            <Skeleton className="h-72 w-full" />
          </div>
        ) : (
          <div className="space-y-10 pb-12">
            <div className="max-w-2xl space-y-10">
              <EnabledSwitch settings={data} />
              <Section title={t('admin.browser.connection')} description={t('admin.browser.connection-hint')}>
                <ConnectionForm settings={data} />
              </Section>
              <Section title={t('admin.browser.test')} description={t('admin.browser.test-hint')}>
                <TryPage />
              </Section>
            </div>
            <Section title={t('admin.browser.actions')} description={t('admin.browser.actions-hint')}>
              <ActionsLog />
            </Section>
          </div>
        )}
      </Main>
    </>
  )
}

export default BrowserSettingsPage

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

function EnabledSwitch({ settings }: { settings: BrowserSettings }) {
  const { t } = useTranslation()
  const update = useUpdateBrowserSettings()
  return (
    <div className="flex flex-row items-center justify-between gap-6 rounded-lg border p-4">
      <div className="space-y-0.5">
        <div className="flex items-center gap-2">
          <Label htmlFor="browser-enabled" className="text-base">
            {t('admin.browser.enabled')}
          </Label>
          <Badge variant={settings.enabled ? 'default' : 'secondary'}>
            {settings.enabled ? t('admin.browser.status-on', { count: settings.sessions }) : t('admin.browser.status-off')}
          </Badge>
        </div>
        <p className="text-muted-foreground text-sm">{t('admin.browser.enabled-hint')}</p>
      </div>
      <Switch
        id="browser-enabled"
        checked={settings.enabled}
        disabled={update.isPending}
        onCheckedChange={(enabled) =>
          update.mutate(
            { enabled },
            {
              onSuccess: () => toast.success(t(enabled ? 'admin.browser.turned-on' : 'admin.browser.turned-off')),
              onError: (error) => toast.error(saveError(error, t('admin.browser.save-failed'))),
            },
          )
        }
      />
    </div>
  )
}

function ConnectionForm({ settings }: { settings: BrowserSettings }) {
  const { t } = useTranslation()
  const update = useUpdateBrowserSettings()

  const schema = z.object({
    endpoint: z.string().trim().min(1, t('admin.browser.required')),
    proxyListen: z.string().trim().min(1, t('admin.browser.required')),
    proxyUrl: z.string().trim().min(1, t('admin.browser.required')),
    proxySources: z.string(),
    maxSessions: z.coerce.number<number>().int(t('admin.browser.sessions-range')).min(1, t('admin.browser.sessions-range')).max(64, t('admin.browser.sessions-range')),
  })
  type Values = z.infer<typeof schema>

  const form = useForm<z.input<typeof schema>, unknown, Values>({
    resolver: zodResolver(schema),
    values: {
      endpoint: settings.endpoint,
      proxyListen: settings.proxyListen,
      proxyUrl: settings.proxyUrl,
      proxySources: settings.proxySources.join('\n'),
      maxSessions: settings.maxSessions,
    },
  })

  const save = (values: Values) =>
    update.mutate(
      {
        ...values,
        proxySources: values.proxySources
          .split(/[\s,]+/)
          .map((s) => s.trim())
          .filter(Boolean),
      },
      {
        onSuccess: () => toast.success(t('admin.browser.saved')),
        onError: (error) => toast.error(saveError(error, t('admin.browser.save-failed'))),
      },
    )

  const text = (name: 'endpoint' | 'proxyListen' | 'proxyUrl', placeholder: string) => (
    <FormField
      control={form.control}
      name={name}
      render={({ field }) => (
        <FormItem>
          <FormLabel>{t(`admin.browser.${name}`)}</FormLabel>
          <FormControl>
            <Input placeholder={placeholder} autoComplete="off" {...field} />
          </FormControl>
          <FormDescription>{t(`admin.browser.${name}-hint`)}</FormDescription>
          <FormMessage />
        </FormItem>
      )}
    />
  )

  return (
    <Form {...form}>
      <form className="space-y-6" onSubmit={form.handleSubmit(save)}>
        {text('endpoint', 'http://browserd:9222')}
        {text('proxyListen', ':8188')}
        {text('proxyUrl', 'http://openchat:8188')}
        <FormField
          control={form.control}
          name="proxySources"
          render={({ field }) => (
            <FormItem>
              <FormLabel>{t('admin.browser.proxySources')}</FormLabel>
              <FormControl>
                <Textarea rows={2} placeholder="172.20.0.0/16" className="font-mono text-sm" {...field} />
              </FormControl>
              <FormDescription>{t('admin.browser.proxySources-hint')}</FormDescription>
              <FormMessage />
            </FormItem>
          )}
        />
        <FormField
          control={form.control}
          name="maxSessions"
          render={({ field }) => (
            <FormItem>
              <FormLabel>{t('admin.browser.maxSessions')}</FormLabel>
              <FormControl>
                <Input type="number" inputMode="numeric" className="w-32" {...field} value={field.value as number | string} />
              </FormControl>
              <FormDescription>{t('admin.browser.maxSessions-hint')}</FormDescription>
              <FormMessage />
            </FormItem>
          )}
        />
        <Button type="submit" disabled={update.isPending || !form.formState.isDirty}>
          {t('admin.browser.save')}
        </Button>
      </form>
    </Form>
  )
}

function TryPage() {
  const { t } = useTranslation()
  const [address, setAddress] = useState('')
  const test = useMutation({
    mutationFn: async (url: string) => unwrap(await api.POST('/admin/browser/test', { body: { url } })),
  })
  return (
    <div className="space-y-2">
      <form
        className="flex gap-2"
        onSubmit={(event) => {
          event.preventDefault()
          if (address.trim()) test.mutate(address.trim())
        }}
      >
        <Input className="flex-1" placeholder="https://example.com" aria-label={t('admin.browser.test')} value={address} onChange={(event) => setAddress(event.target.value)} />
        <Button type="submit" variant="outline" disabled={test.isPending || !address.trim()}>
          {test.isPending && <Loader2 className="animate-spin" aria-hidden="true" />}
          {t('admin.browser.test-run')}
        </Button>
      </form>
      {test.isError && (
        <Alert variant="destructive">
          <XCircle aria-hidden="true" />
          <AlertTitle>{t('admin.browser.test-failed')}</AlertTitle>
        </Alert>
      )}
      {test.data &&
        (test.data.ok ? (
          <Alert>
            <CheckCircle2 aria-hidden="true" />
            <AlertTitle>{t('admin.browser.test-ok', { ms: test.data.latencyMs })}</AlertTitle>
            <AlertDescription>
              {test.data.title && <p className="font-medium">{test.data.title}</p>}
              <pre className="line-clamp-6 text-xs break-all whitespace-pre-wrap">{test.data.preview}</pre>
            </AlertDescription>
          </Alert>
        ) : (
          <Alert variant="destructive">
            <XCircle aria-hidden="true" />
            <AlertTitle>{t('admin.browser.test-failed')}</AlertTitle>
            {/* Written for the model, in English, but the only account there is. */}
            <AlertDescription>{test.data.error}</AlertDescription>
          </Alert>
        ))}
    </div>
  )
}

/** The actions that needed asking (browser_actions), paged by cursor as the other logs are. */
function ActionsLog() {
  const { t } = useTranslation()
  const [pageSize, setPageSize] = useState(20)
  const [trail, setTrail] = useState<{ cursors: (string | undefined)[]; index: number }>({ cursors: [undefined], index: 0 })
  const cursor = trail.cursors[trail.index]
  const { data, isPending, isError, isFetching } = useQuery({
    queryKey: adminKeys.browserActions({ cursor, pageSize }),
    queryFn: async ({ signal }) => unwrap(await api.GET('/admin/browser/actions', { params: { query: { cursor, limit: pageSize } }, signal })),
    placeholderData: keepPreviousData,
  })
  const backToStart = useCallback(() => setTrail({ cursors: [undefined], index: 0 }), [])
  const rows = data?.items ?? []

  return (
    <div className="space-y-4">
      <div className="overflow-hidden rounded-md border">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>{t('admin.browser.col-time')}</TableHead>
              <TableHead>{t('admin.browser.col-who')}</TableHead>
              <TableHead>{t('admin.browser.col-site')}</TableHead>
              <TableHead>{t('admin.browser.col-action')}</TableHead>
              <TableHead>{t('admin.browser.col-outcome')}</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {isError && (
              <TableRow>
                <TableCell colSpan={5} className="h-24 text-center">
                  {t('state.error')}
                </TableCell>
              </TableRow>
            )}
            {!isError && isPending && (
              <TableRow>
                <TableCell colSpan={5}>
                  <Skeleton className="h-9 w-full" />
                </TableCell>
              </TableRow>
            )}
            {!isError && !isPending && rows.length === 0 && (
              <TableRow>
                <TableCell colSpan={5} className="text-muted-foreground h-24 text-center">
                  {t('admin.browser.actions-none')}
                </TableCell>
              </TableRow>
            )}
            {rows.map((row) => (
              <TableRow key={row.id}>
                <TableCell className="font-mono text-xs whitespace-nowrap">{formatLogTime(row.createdAt)}</TableCell>
                <TableCell>
                  <LongText className="max-w-36">{row.userName || row.userEmail || t('admin.usage.no-account')}</LongText>
                </TableCell>
                <TableCell className="font-mono text-xs">{row.site}</TableCell>
                <TableCell>
                  <div className="flex flex-col">
                    <span className="text-sm">{t(`admin.browser.action.${row.action}`, { defaultValue: row.action })}</span>
                    <LongText className="text-muted-foreground max-w-64 text-xs">{row.target}</LongText>
                  </div>
                </TableCell>
                <TableCell>
                  <Badge variant={row.outcome === 'allowed' || row.outcome === 'granted' ? 'secondary' : 'outline'}>
                    {t(`admin.browser.outcome.${row.outcome}`, { defaultValue: row.outcome })}
                  </Badge>
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </div>
      <DataTableCursorPagination
        page={trail.index + 1}
        pageSize={pageSize}
        onPageSizeChange={(size) => {
          setPageSize(size)
          backToStart()
        }}
        canPrevious={trail.index > 0}
        canNext={Boolean(data?.nextCursor) && !isFetching}
        onFirst={backToStart}
        onPrevious={() => setTrail((current) => ({ ...current, index: Math.max(0, current.index - 1) }))}
        onNext={() => {
          const next = data?.nextCursor
          if (next) setTrail((current) => ({ cursors: [...current.cursors.slice(0, current.index + 1), next], index: current.index + 1 }))
        }}
      />
    </div>
  )
}
