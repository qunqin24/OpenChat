import { ArrowDown, ArrowUp, CheckCircle2, ExternalLink, Loader2, XCircle } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { ConfigDrawer } from '@/components/shadcn/config-drawer'
import { Header } from '@/components/shadcn/layout/header'
import { Main } from '@/components/shadcn/layout/main'
import { PasswordInput } from '@/components/shadcn/password-input'
import { ProfileDropdown } from '@/components/shadcn/profile-dropdown'
import { Search } from '@/components/shadcn/search'
import { Alert, AlertDescription, AlertTitle } from '@/components/shadcn/ui/alert'
import { Badge } from '@/components/shadcn/ui/badge'
import { Button } from '@/components/shadcn/ui/button'
import { Checkbox } from '@/components/shadcn/ui/checkbox'
import { Input } from '@/components/shadcn/ui/input'
import { Label } from '@/components/shadcn/ui/label'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/shadcn/ui/select'
import { Separator } from '@/components/shadcn/ui/separator'
import { Skeleton } from '@/components/shadcn/ui/skeleton'
import { Switch } from '@/components/shadcn/ui/switch'
import {
  useImageSearchSettings,
  useTestImageSearch,
  useUpdateImageSearchSettings,
  type ImageSearchProvider,
  type ImageSearchSettings,
  type ImageSearchTestResult,
} from './use-image-search'

const PROVIDERS: ImageSearchProvider[] = ['pixabay', 'wikimedia', 'bing', 'openverse']
const PROVIDER_NAMES: Record<ImageSearchProvider, string> = {
  wikimedia: 'Wikimedia Commons',
  openverse: 'Openverse',
  pixabay: 'Pixabay',
  bing: 'Bing Images',
}

/**
 * Image search: whether answers describe their pictures for the server to find
 * (api/pictures.go) or may only show pictures their sources listed, which
 * services find them and in what order - the next is asked when one is down
 * or finds nothing - and Pixabay's key. Laid out like the web search page: the
 * switch and the services save the moment they change, the key has its own
 * save, and a search can be tried before anything is saved.
 */
export function ImageSearch() {
  const { t } = useTranslation()
  const { data, isPending } = useImageSearchSettings()

  return (
    <>
      <Header fixed>
        <Search className="me-auto" />
        <ConfigDrawer />
        <ProfileDropdown />
      </Header>

      <Main className="flex flex-1 flex-col">
        <div>
          <h2 className="text-2xl font-bold tracking-tight">{t('admin.image-search.title')}</h2>
          <p className="text-muted-foreground">{t('admin.image-search.subtitle')}</p>
        </div>
        <Separator className="my-4 lg:my-6" />

        {isPending || !data ? (
          <div className="max-w-2xl space-y-4">
            <Skeleton className="h-20 w-full" />
            <Skeleton className="h-40 w-full" />
          </div>
        ) : (
          <div className="max-w-2xl space-y-10 pb-12">
            <EnabledSwitch settings={data} />
            <section className="space-y-6">
              <div>
                <h3 className="text-lg font-medium">{t('admin.image-search.provider')}</h3>
                <p className="text-muted-foreground text-sm">{t('admin.image-search.provider-hint')}</p>
              </div>
              <ProviderFields settings={data} />
            </section>
          </div>
        )}
      </Main>
    </>
  )
}

export default ImageSearch

/** On or off, and why it might be off despite the switch. */
function EnabledSwitch({ settings }: { settings: ImageSearchSettings }) {
  const { t } = useTranslation()
  const update = useUpdateImageSearchSettings()
  const noKey = settings.providers.every((provider) => provider === 'pixabay') && !settings.pixabayKeySet
  const live = settings.enabled && !noKey

  return (
    <div className="flex flex-row items-center justify-between gap-6 rounded-lg border p-4">
      <div className="space-y-0.5">
        <div className="flex items-center gap-2">
          <Label htmlFor="image-search-enabled" className="text-base">
            {t('admin.image-search.enabled')}
          </Label>
          <Badge variant={live ? 'default' : 'secondary'}>
            {live
              ? t('admin.image-search.status-live')
              : settings.enabled && noKey
                ? t('admin.image-search.status-no-key')
                : t('admin.image-search.status-off')}
          </Badge>
        </div>
        <p className="text-muted-foreground text-sm">
          {live ? t('admin.image-search.status-live-hint') : t('admin.image-search.status-off-hint')}
        </p>
      </div>
      <Switch
        id="image-search-enabled"
        checked={settings.enabled}
        disabled={update.isPending}
        onCheckedChange={(enabled) =>
          update.mutate(
            { enabled },
            {
              onSuccess: () => toast.success(t(enabled ? 'admin.image-search.turned-on' : 'admin.image-search.turned-off')),
              onError: () => toast.error(t('admin.image-search.save-failed')),
            },
          )
        }
      />
    </div>
  )
}

/** The services in order, Pixabay's key, and a search to prove they work. */
function ProviderFields({ settings }: { settings: ImageSearchSettings }) {
  const { t } = useTranslation()
  const update = useUpdateImageSearchSettings()
  const test = useTestImageSearch()
  const [key, setKey] = useState('')
  const [query, setQuery] = useState('')
  const [tried, setTried] = useState<ImageSearchProvider | 'all'>('all')
  const used = settings.providers
  const pixabay = used.includes('pixabay')
  // Those in use, in their order, then the rest.
  const rows = [...used, ...PROVIDERS.filter((provider) => !used.includes(provider))]

  const saveProviders = (providers: ImageSearchProvider[]) =>
    update.mutate(
      { providers },
      {
        onSuccess: () => {
          test.reset()
          toast.success(t('admin.image-search.saved'))
        },
        onError: () => toast.error(t('admin.image-search.save-failed')),
      },
    )
  const toggle = (provider: ImageSearchProvider, on: boolean) =>
    saveProviders(on ? [...used, provider] : used.filter((other) => other !== provider))
  const move = (index: number, by: -1 | 1) => {
    const next = used.filter((_, at) => at !== index)
    next.splice(index + by, 0, used[index] as ImageSearchProvider)
    saveProviders(next)
  }

  const saveKey = (pixabayKey: string) =>
    update.mutate(
      { pixabayKey },
      {
        onSuccess: (next) => {
          setKey('')
          toast.success(t(next.pixabayKeySet ? 'admin.image-search.key-saved' : 'admin.image-search.key-removed'))
        },
        onError: () => toast.error(t('admin.image-search.save-failed')),
      },
    )

  return (
    <div className="space-y-8">
      <div className="space-y-2">
        <Label>{t('admin.image-search.service')}</Label>
        <ol className="divide-y rounded-lg border">
          {rows.map((provider) => {
            const index = used.indexOf(provider)
            const on = index >= 0
            return (
              <li key={provider} className="flex items-start gap-3 p-4">
                <Checkbox
                  id={`image-search-${provider}`}
                  className="mt-0.5"
                  checked={on}
                  // One service always stays.
                  disabled={update.isPending || (on && used.length === 1)}
                  onCheckedChange={(checked) => toggle(provider, checked === true)}
                />
                <div className="min-w-0 flex-1 space-y-1">
                  <Label htmlFor={`image-search-${provider}`} className="flex items-center gap-2">
                    {on && <span className="text-muted-foreground tabular-nums">{index + 1}.</span>}
                    {PROVIDER_NAMES[provider]}
                    {on && provider === 'pixabay' && !settings.pixabayKeySet && (
                      <Badge variant="secondary">{t('admin.image-search.status-no-key')}</Badge>
                    )}
                  </Label>
                  <p className="text-muted-foreground text-sm">{t(`admin.image-search.hint-${provider}`)}</p>
                </div>
                {on && used.length > 1 && (
                  <div className="flex gap-1">
                    <Button
                      type="button"
                      variant="ghost"
                      size="icon"
                      className="size-8"
                      aria-label={t('admin.image-search.move-up')}
                      disabled={update.isPending || index === 0}
                      onClick={() => move(index, -1)}
                    >
                      <ArrowUp />
                    </Button>
                    <Button
                      type="button"
                      variant="ghost"
                      size="icon"
                      className="size-8"
                      aria-label={t('admin.image-search.move-down')}
                      disabled={update.isPending || index === used.length - 1}
                      onClick={() => move(index, 1)}
                    >
                      <ArrowDown />
                    </Button>
                  </div>
                )}
              </li>
            )
          })}
        </ol>
        <p className="text-muted-foreground text-sm">{t('admin.image-search.order-hint')}</p>
      </div>

      {pixabay && (
        <div className="space-y-2">
          <Label htmlFor="image-search-key">{t('admin.image-search.key')}</Label>
          <form
            className="flex gap-2"
            onSubmit={(event) => {
              event.preventDefault()
              if (key.trim()) saveKey(key.trim())
            }}
          >
            <PasswordInput
              id="image-search-key"
              className="flex-1"
              autoComplete="off"
              placeholder={settings.pixabayKeyPreview ?? ''}
              value={key}
              disabled={update.isPending}
              onChange={(event) => setKey(event.target.value)}
            />
            <Button type="submit" disabled={!key.trim() || update.isPending}>
              {t('admin.image-search.save-key')}
            </Button>
            {settings.pixabayKeySet && (
              <Button type="button" variant="outline" disabled={update.isPending} onClick={() => saveKey('')}>
                {t('admin.channels.api-key-clear')}
              </Button>
            )}
          </form>
          <p className="text-muted-foreground text-sm">
            {settings.pixabayKeySet ? t('admin.image-search.key-stored') : t('admin.image-search.key-missing')}{' '}
            <a
              href="https://pixabay.com/api/docs/"
              target="_blank"
              rel="noreferrer"
              className="text-foreground inline-flex items-center gap-1 underline-offset-4 hover:underline"
            >
              {t('admin.image-search.get-key')}
              <ExternalLink className="size-3" aria-hidden="true" />
            </a>
          </p>
        </div>
      )}

      <div className="space-y-2">
        <Label htmlFor="image-search-test">{t('admin.image-search.test')}</Label>
        <form
          className="flex gap-2"
          onSubmit={(event) => {
            event.preventDefault()
            if (!query.trim()) return
            test.mutate({
              query: query.trim(),
              ...(tried !== 'all' ? { provider: tried } : {}),
              ...(pixabay && key.trim() ? { pixabayKey: key.trim() } : {}),
            })
          }}
        >
          <Select value={tried} onValueChange={(value) => setTried(value as ImageSearchProvider | 'all')}>
            <SelectTrigger className="w-[200px]" aria-label={t('admin.image-search.test-service')}>
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="all">{t('admin.image-search.test-all')}</SelectItem>
              {PROVIDERS.map((provider) => (
                <SelectItem key={provider} value={provider}>
                  {PROVIDER_NAMES[provider]}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          <Input
            id="image-search-test"
            className="flex-1"
            placeholder="laptop keyboard"
            value={query}
            onChange={(event) => setQuery(event.target.value)}
          />
          <Button type="submit" variant="outline" disabled={test.isPending || !query.trim()}>
            {test.isPending && <Loader2 className="animate-spin" />}
            {t('admin.image-search.test-run')}
          </Button>
        </form>
        <p className="text-muted-foreground text-sm">{t('admin.image-search.test-hint')}</p>
        {test.isError && (
          <Alert variant="destructive">
            <XCircle />
            <AlertTitle>{t('admin.image-search.test-failed')}</AlertTitle>
          </Alert>
        )}
        {test.data && <TestResult result={test.data} />}
      </div>
    </div>
  )
}

function TestResult({ result }: { result: ImageSearchTestResult }) {
  const { t } = useTranslation()
  if (!result.ok) {
    return (
      <Alert variant="destructive">
        <XCircle />
        <AlertTitle>{t('admin.image-search.test-failed')}</AlertTitle>
        {result.error && <AlertDescription className="break-all">{result.error}</AlertDescription>}
      </Alert>
    )
  }
  const image = result.image
  return (
    <Alert>
      <CheckCircle2 />
      <AlertTitle>
        {image ? t('admin.image-search.test-found', { ms: result.latencyMs }) : t('admin.image-search.test-none', { ms: result.latencyMs })}
      </AlertTitle>
      {image && (
        <AlertDescription className="space-y-1">
          <div className="text-foreground font-medium break-all">{image.title || '—'}</div>
          <div>
            {[PROVIDER_NAMES[image.provider], image.creator, image.license, image.source, `${image.width}×${image.height}`]
              .filter(Boolean)
              .join(' · ')}
          </div>
          {image.landingUrl && (
            <a href={image.landingUrl} target="_blank" rel="noreferrer" className="text-foreground inline-flex items-center gap-1 break-all underline-offset-4 hover:underline">
              {image.landingUrl}
              <ExternalLink className="size-3 shrink-0" aria-hidden="true" />
            </a>
          )}
        </AlertDescription>
      )}
    </Alert>
  )
}
