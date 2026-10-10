import { OpenChatError } from '@openchat/api-client'
import { zodResolver } from '@hookform/resolvers/zod'
import { CheckCircle2, ExternalLink, Loader2, XCircle } from 'lucide-react'
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
import { Badge } from '@/components/shadcn/ui/badge'
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
import { Label } from '@/components/shadcn/ui/label'
import { RadioGroup, RadioGroupItem } from '@/components/shadcn/ui/radio-group'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/shadcn/ui/select'
import { Separator } from '@/components/shadcn/ui/separator'
import { Skeleton } from '@/components/shadcn/ui/skeleton'
import { Switch } from '@/components/shadcn/ui/switch'
import { Textarea } from '@/components/shadcn/ui/textarea'
import {
  useTestWebSearch,
  useUpdateWebSearchSettings,
  useWebSearchSettings,
  type WebSearchSettings,
  type WebSearchTestResult,
} from './use-web-search'

/** The free search's services, by the id the server reports. */
const VENDOR_NAMES: Record<string, string> = {
  exa: 'Exa',
  parallel: 'Parallel',
  firecrawl: 'Firecrawl',
  keenable: 'Keenable',
}

const RESULT_COUNTS = Array.from({ length: 20 }, (_, i) => i + 1)

const DEPTHS = [
  { value: 'basic', label: 'admin.web-search.depth-basic', hint: 'admin.web-search.depth-basic-hint' },
  { value: 'advanced', label: 'admin.web-search.depth-advanced', hint: 'admin.web-search.depth-advanced-hint' },
] as const

const COMPRESSIONS = [
  { value: 'snippet', label: 'admin.web-search.compress-snippet', hint: 'admin.web-search.compress-snippet-hint' },
  { value: 'truncate', label: 'admin.web-search.compress-truncate', hint: 'admin.web-search.compress-truncate-hint' },
  { value: 'none', label: 'admin.web-search.compress-none', hint: 'admin.web-search.compress-none-hint' },
] as const

/**
 * Web search: the service and its key, how each search runs, and the sites
 * that are ruled out. Its own page rather than a row in system settings,
 * because it has grown into a policy — what a search costs in credits and in
 * the model's context, and what it is allowed to read.
 *
 * Laid out the way the settings pages are: titled sections, label above
 * control, a description under it. Three parts save separately, on purpose.
 * The switch takes effect the moment it moves, like the other switches in this
 * panel. The key is write-only and has its own save, so changing the result
 * count never sends a half-typed key. Everything else is one form, one save.
 */
export function WebSearch() {
  const { t } = useTranslation()
  const { data, isPending } = useWebSearchSettings()

  return (
    <>
      <Header fixed>
        <Search className="me-auto" />
        <ConfigDrawer />
        <ProfileDropdown />
      </Header>

      <Main className="flex flex-1 flex-col">
        <div>
          <h2 className="text-2xl font-bold tracking-tight">{t('admin.web-search.title')}</h2>
          <p className="text-muted-foreground">{t('admin.web-search.subtitle')}</p>
        </div>
        <Separator className="my-4 lg:my-6" />

        {isPending || !data ? (
          <div className="max-w-2xl space-y-4">
            <Skeleton className="h-20 w-full" />
            <Skeleton className="h-40 w-full" />
            <Skeleton className="h-72 w-full" />
          </div>
        ) : (
          <div className="max-w-2xl space-y-10 pb-12">
            <EnabledSwitch settings={data} />
            <Section title={t('admin.web-search.provider')} description={t('admin.web-search.provider-hint')}>
              <ProviderFields settings={data} />
            </Section>
            <PolicyForm settings={data} />
          </div>
        )}
      </Main>
    </>
  )
}

export default WebSearch

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

/** On or off, and why it might be off despite the switch. */
function EnabledSwitch({ settings }: { settings: WebSearchSettings }) {
  const { t } = useTranslation()
  const update = useUpdateWebSearchSettings()
  const isFree = settings.provider === 'free'
  // The free search needs no key; Tavily does.
  const live = settings.enabled && (isFree || settings.keySet)

  return (
    <div className="flex flex-row items-center justify-between gap-6 rounded-lg border p-4">
      <div className="space-y-0.5">
        <div className="flex items-center gap-2">
          <Label htmlFor="web-search-enabled" className="text-base">
            {t('admin.web-search.enabled')}
          </Label>
          <Badge variant={live ? 'default' : 'secondary'}>
            {live
              ? t('admin.web-search.status-live')
              : settings.enabled && !isFree && !settings.keySet
                ? t('admin.web-search.status-no-key')
                : t('admin.web-search.status-off')}
          </Badge>
        </div>
        <p className="text-muted-foreground text-sm">
          {live ? t('admin.web-search.status-live-hint') : t('admin.web-search.status-off-hint')}
        </p>
      </div>
      <Switch
        id="web-search-enabled"
        checked={settings.enabled}
        disabled={update.isPending}
        onCheckedChange={(enabled) =>
          update.mutate(
            { enabled },
            {
              onSuccess: () =>
                toast.success(t(enabled ? 'admin.web-search.turned-on' : 'admin.web-search.turned-off')),
              onError: () => toast.error(t('admin.web-search.save-failed')),
            },
          )
        }
      />
    </div>
  )
}

/** The service, its key, and a search to prove they work. */
function ProviderFields({ settings }: { settings: WebSearchSettings }) {
  const { t } = useTranslation()
  const update = useUpdateWebSearchSettings()
  const test = useTestWebSearch()
  const [key, setKey] = useState('')
  const [query, setQuery] = useState('')
  const isFree = settings.provider === 'free'

  const changeProvider = (provider: WebSearchSettings['provider']) =>
    update.mutate(
      { provider },
      {
        onSuccess: () => {
          test.reset()
          toast.success(t('admin.web-search.saved'))
        },
        onError: () => toast.error(t('admin.web-search.save-failed')),
      },
    )

  const saveKey = (apiKey: string) =>
    update.mutate(
      { apiKey },
      {
        onSuccess: (next) => {
          setKey('')
          toast.success(t(next.keySet ? 'admin.web-search.key-saved' : 'admin.web-search.key-removed'))
        },
        onError: () => toast.error(t('admin.web-search.save-failed')),
      },
    )

  return (
    <div className="space-y-8">
      {/* Saved the moment it changes, like the switch: the fields below it
          depend on which service it is. */}
      <div className="space-y-2">
        <Label htmlFor="web-search-provider">{t('admin.web-search.service')}</Label>
        <Select value={settings.provider} onValueChange={changeProvider} disabled={update.isPending}>
          <SelectTrigger id="web-search-provider" className="w-[240px]">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="free">{t('admin.web-search.provider-free')}</SelectItem>
            <SelectItem value="tavily">Tavily</SelectItem>
          </SelectContent>
        </Select>
        <p className="text-muted-foreground text-sm">
          {t(isFree ? 'admin.web-search.free-hint' : 'admin.web-search.tavily-hint')}
        </p>
      </div>

      {!isFree && (
        <div className="space-y-2">
          <Label htmlFor="web-search-key">{t('admin.web-search.key')}</Label>
          <form
            className="flex gap-2"
            onSubmit={(event) => {
              event.preventDefault()
              if (key.trim()) saveKey(key.trim())
            }}
          >
            <PasswordInput
              id="web-search-key"
              className="flex-1"
              autoComplete="off"
              placeholder={settings.keyPreview ?? 'tvly-…'}
              value={key}
              disabled={update.isPending}
              onChange={(event) => setKey(event.target.value)}
            />
            <Button type="submit" disabled={!key.trim() || update.isPending}>
              {t('admin.web-search.save-key')}
            </Button>
            {settings.keySet && (
              <Button type="button" variant="outline" disabled={update.isPending} onClick={() => saveKey('')}>
                {t('admin.channels.api-key-clear')}
              </Button>
          )}
        </form>
        <p className="text-muted-foreground text-sm">
          {settings.keySet ? t('admin.web-search.key-stored') : t('admin.web-search.key-missing')}{' '}
          <a
            href="https://app.tavily.com/home"
            target="_blank"
            rel="noreferrer"
            className="text-foreground inline-flex items-center gap-1 underline-offset-4 hover:underline"
          >
            {t('admin.web-search.get-key')}
            <ExternalLink className="size-3" aria-hidden="true" />
          </a>
        </p>
      </div>
      )}

      <div className="space-y-2">
        <Label htmlFor="web-search-test">{t('admin.web-search.test')}</Label>
        <form
          className="flex gap-2"
          onSubmit={(event) => {
            event.preventDefault()
            test.mutate({
              ...(query.trim() ? { query: query.trim() } : {}),
              ...(!isFree && key.trim() ? { apiKey: key.trim() } : {}),
            })
          }}
        >
          <Input
            id="web-search-test"
            className="flex-1"
            placeholder={t('admin.web-search.test-placeholder')}
            value={query}
            onChange={(event) => setQuery(event.target.value)}
          />
          <Button
            type="submit"
            variant="outline"
            disabled={test.isPending || (!isFree && !settings.keySet && !key.trim())}
          >
            {test.isPending && <Loader2 className="animate-spin" aria-hidden="true" />}
            {t('admin.web-search.test-run')}
          </Button>
        </form>
        <p className="text-muted-foreground text-sm">{t('admin.web-search.test-hint')}</p>
        {test.data && <TestOutcome result={test.data} />}
        {test.isError && (
          <Alert variant="destructive">
            <XCircle aria-hidden="true" />
            <AlertTitle>{t('admin.web-search.save-failed')}</AlertTitle>
          </Alert>
        )}
      </div>
    </div>
  )
}

function TestOutcome({ result }: { result: WebSearchTestResult }) {
  const { t } = useTranslation()
  if (!result.ok) {
    return (
      <Alert variant="destructive">
        <XCircle aria-hidden="true" />
        <AlertTitle>{t('admin.web-search.test-failed')}</AlertTitle>
        {/* The service's own words: for an administrator chasing a bad key or
            a spent quota, the exact reason is the useful part. */}
        {result.error && <AlertDescription>{result.error}</AlertDescription>}
      </Alert>
    )
  }
  return (
    <Alert>
      <CheckCircle2 aria-hidden="true" />
      <AlertTitle>
        {t('admin.web-search.test-ok')} ·{' '}
        <span className="text-muted-foreground font-normal tabular-nums">
          {t('admin.web-search.test-summary', { ms: result.latencyMs, count: result.results.length })}
          {result.blocked ? ` · ${t('admin.web-search.test-blocked', { count: result.blocked })}` : ''}
          {result.servedBy
            ? ` · ${t('admin.web-search.test-served-by', { name: VENDOR_NAMES[result.servedBy] ?? result.servedBy })}`
            : ''}
        </span>
      </AlertTitle>
      {result.results.length > 0 && (
        <AlertDescription>
          <ol className="mt-1 w-full space-y-2">
            {result.results.map((hit, i) => (
              <li key={hit.url + i} className="min-w-0">
                <a
                  href={hit.url}
                  target="_blank"
                  rel="noreferrer"
                  className="text-foreground line-clamp-1 font-medium underline-offset-4 hover:underline"
                >
                  {hit.title || hit.url}
                </a>
                <span className="line-clamp-1 text-xs">
                  {hit.url} · {t('admin.web-search.test-chars', { count: hit.content.length })}
                </span>
              </li>
            ))}
          </ol>
        </AlertDescription>
      )}
    </Alert>
  )
}

/** A blocklist rule as the server reads it: blank, /regex/, *.domain or domain. */
function validRule(raw: string): boolean {
  const rule = raw.trim()
  if (rule === '') return true
  if (rule.length > 2 && rule.startsWith('/') && rule.endsWith('/')) {
    try {
      new RegExp(rule.slice(1, -1))
      return true
    } catch {
      return false
    }
  }
  const domain = rule.startsWith('*.') ? rule.slice(2) : rule
  try {
    const host = new URL(domain.includes('://') ? domain : `http://${domain}`).hostname
    return host !== '' && !/[\s*]/.test(host)
  } catch {
    return false
  }
}

/** How searches run and what they may return: one form, one save. */
function PolicyForm({ settings }: { settings: WebSearchSettings }) {
  const { t } = useTranslation()
  const update = useUpdateWebSearchSettings()

  const schema = z.object({
    maxResults: z.string(),
    depth: z.enum(['basic', 'advanced']),
    compression: z.enum(['snippet', 'truncate', 'none']),
    truncateLength: z
      .string()
      .trim()
      .refine((value) => /^\d+$/.test(value) && Number(value) >= 200 && Number(value) <= 20000, {
        message: t('admin.web-search.range', { min: 200, max: 20000 }),
      }),
    // Checked here as the server checks it, so a bad rule is named on its own
    // line in this language rather than in the server's English.
    blocklist: z.string().superRefine((value, ctx) => {
      const lines = value.split('\n')
      const bad = lines.findIndex((line) => !validRule(line))
      if (bad >= 0) {
        ctx.addIssue({
          code: 'custom',
          message: t('admin.web-search.blocklist-invalid', { line: bad + 1, rule: lines[bad]!.trim() }),
        })
      }
    }),
  })
  type Values = z.infer<typeof schema>

  const valuesOf = (s: WebSearchSettings): Values => ({
    maxResults: String(s.maxResults),
    depth: s.depth,
    compression: s.compression,
    truncateLength: String(s.truncateLength),
    blocklist: s.blocklist.join('\n'),
  })
  const form = useForm<Values>({ resolver: zodResolver(schema), defaultValues: valuesOf(settings) })
  const compression = useWatch({ control: form.control, name: 'compression' })

  const submit = (values: Values) =>
    update.mutate(
      {
        maxResults: Number(values.maxResults),
        depth: values.depth,
        compression: values.compression,
        truncateLength: Number(values.truncateLength),
        blocklist: values.blocklist
          .split('\n')
          .map((line) => line.trim())
          .filter(Boolean),
      },
      {
        onSuccess: (next) => {
          form.reset(valuesOf(next))
          toast.success(t('admin.web-search.saved'))
        },
        onError: (error) => {
          // The check above should have caught it; a rule the server's regular
          // expressions reject but the browser's accept still lands on the field.
          if (error instanceof OpenChatError && error.code === 'invalid_blocklist') {
            form.setError('blocklist', { message: t('admin.web-search.blocklist-rejected') })
            return
          }
          toast.error(t('admin.web-search.save-failed'))
        },
      },
    )

  return (
    <Form {...form}>
      <form className="space-y-10" onSubmit={form.handleSubmit(submit)}>
        <Section title={t('admin.web-search.results')} description={t('admin.web-search.results-hint')}>
          <div className="space-y-8">
            <FormField
              control={form.control}
              name="maxResults"
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('admin.web-search.max-results')}</FormLabel>
                  <Select value={field.value} onValueChange={field.onChange}>
                    <FormControl>
                      <SelectTrigger className="w-[120px]">
                        <SelectValue />
                      </SelectTrigger>
                    </FormControl>
                    <SelectContent>
                      {RESULT_COUNTS.map((count) => (
                        <SelectItem key={count} value={String(count)}>
                          {count}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                  <FormDescription>{t('admin.web-search.max-results-hint')}</FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />

            <FormField
              control={form.control}
              name="depth"
              render={({ field }) => (
                <FormItem className="space-y-3">
                  <FormLabel>{t('admin.web-search.depth')}</FormLabel>
                  {/* Depth is Tavily's notion; the free services have none. */}
                  {settings.provider === 'free' && (
                    <FormDescription>{t('admin.web-search.depth-free')}</FormDescription>
                  )}
                  <FormControl>
                    <RadioGroup
                      value={field.value}
                      onValueChange={field.onChange}
                      disabled={settings.provider === 'free'}
                      className="gap-3"
                    >
                      {DEPTHS.map((option) => (
                        <RadioOption key={option.value} value={option.value} label={t(option.label)} hint={t(option.hint)} />
                      ))}
                    </RadioGroup>
                  </FormControl>
                </FormItem>
              )}
            />

            <FormField
              control={form.control}
              name="compression"
              render={({ field }) => (
                <FormItem className="space-y-3">
                  <FormLabel>{t('admin.web-search.compression')}</FormLabel>
                  <FormDescription>
                    {t('admin.web-search.compression-hint')}
                    {settings.provider === 'free' ? ` ${t('admin.web-search.compression-free')}` : ''}
                  </FormDescription>
                  <FormControl>
                    <RadioGroup value={field.value} onValueChange={field.onChange} className="gap-3">
                      {COMPRESSIONS.map((option) => (
                        <RadioOption key={option.value} value={option.value} label={t(option.label)} hint={t(option.hint)} />
                      ))}
                    </RadioGroup>
                  </FormControl>
                </FormItem>
              )}
            />

            {compression === 'truncate' && (
              <FormField
                control={form.control}
                name="truncateLength"
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('admin.web-search.truncate-length')}</FormLabel>
                    <FormControl>
                      <Input inputMode="numeric" className="w-[160px]" {...field} />
                    </FormControl>
                    <FormDescription>{t('admin.web-search.truncate-length-hint')}</FormDescription>
                    <FormMessage />
                  </FormItem>
                )}
              />
            )}
          </div>
        </Section>

        <Section title={t('admin.web-search.blocklist')} description={t('admin.web-search.blocklist-hint')}>
          <FormField
            control={form.control}
            name="blocklist"
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('admin.web-search.blocklist-rules')}</FormLabel>
                <FormControl>
                  <Textarea
                    rows={6}
                    spellCheck={false}
                    className="font-mono text-sm"
                    placeholder={'example.com\n*.ads.example.net\n/\\.pdf$/'}
                    {...field}
                  />
                </FormControl>
                <FormDescription>
                  {t('admin.web-search.blocklist-syntax')}
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />
        </Section>

        <div className="flex gap-2">
          <Button type="submit" disabled={!form.formState.isDirty || update.isPending}>
            {update.isPending && <Loader2 className="animate-spin" aria-hidden="true" />}
            {t('admin.web-search.save')}
          </Button>
          {form.formState.isDirty && (
            <Button type="button" variant="outline" disabled={update.isPending} onClick={() => form.reset(valuesOf(settings))}>
              {t('admin.ui.cancel')}
            </Button>
          )}
        </div>
      </form>
    </Form>
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
