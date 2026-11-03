import { CheckCircle2, ExternalLink, Loader2, XCircle } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { PasswordInput } from '@/components/shadcn/password-input'
import { Alert, AlertDescription, AlertTitle } from '@/components/shadcn/ui/alert'
import { Button } from '@/components/shadcn/ui/button'
import { Input } from '@/components/shadcn/ui/input'
import { Label } from '@/components/shadcn/ui/label'
import { RadioGroup, RadioGroupItem } from '@/components/shadcn/ui/radio-group'
import { Skeleton } from '@/components/shadcn/ui/skeleton'
import { useTestWebFetch, useUpdateWebFetchSettings, useWebFetchSettings, type WebFetchSettings } from './use-web-search'

const MODES = [
  { value: 'fallback', label: 'admin.web-fetch.mode-fallback', hint: 'admin.web-fetch.mode-fallback-hint' },
  { value: 'builtin', label: 'admin.web-fetch.mode-builtin', hint: 'admin.web-fetch.mode-builtin-hint' },
  { value: 'jina', label: 'admin.web-fetch.mode-jina', hint: 'admin.web-fetch.mode-jina-hint' },
] as const

/**
 * How web_fetch reads a page (api/web_fetch.go): itself, itself with Jina
 * Reader when a site turns it away, or Jina first; Jina's key; and a page to
 * try. The mode saves the moment it changes, like the search service; the key
 * is write-only with its own save.
 */
export function WebFetchSection() {
  const { t } = useTranslation()
  const { data, isPending } = useWebFetchSettings()
  return (
    <section className="space-y-6">
      <div>
        <h3 className="text-lg font-medium">{t('admin.web-fetch.title')}</h3>
        <p className="text-muted-foreground text-sm">{t('admin.web-fetch.subtitle')}</p>
      </div>
      {isPending || !data ? <Skeleton className="h-48 w-full" /> : <WebFetchFields settings={data} />}
    </section>
  )
}

function WebFetchFields({ settings }: { settings: WebFetchSettings }) {
  const { t } = useTranslation()
  const update = useUpdateWebFetchSettings()
  const test = useTestWebFetch()
  const [key, setKey] = useState('')
  const [address, setAddress] = useState('')
  const usesJina = settings.reader !== 'builtin'

  const saveKey = (jinaKey: string) =>
    update.mutate(
      { jinaKey },
      {
        onSuccess: (next) => {
          setKey('')
          toast.success(t(next.jinaKeySet ? 'admin.web-search.key-saved' : 'admin.web-search.key-removed'))
        },
        onError: () => toast.error(t('admin.web-search.save-failed')),
      },
    )

  return (
    <div className="space-y-8">
      <RadioGroup
        value={settings.reader}
        disabled={update.isPending}
        onValueChange={(reader) =>
          update.mutate(
            { reader: reader as WebFetchSettings['reader'] },
            {
              onSuccess: () => {
                test.reset()
                toast.success(t('admin.web-search.saved'))
              },
              onError: () => toast.error(t('admin.web-search.save-failed')),
            },
          )
        }
        className="space-y-3"
      >
        {MODES.map((mode) => (
          <div key={mode.value} className="flex items-start gap-3">
            <RadioGroupItem id={`web-fetch-${mode.value}`} value={mode.value} className="mt-0.5" />
            <div className="space-y-1 leading-none">
              <Label htmlFor={`web-fetch-${mode.value}`} className="font-normal">
                {t(mode.label)}
              </Label>
              <p className="text-muted-foreground text-sm">{t(mode.hint)}</p>
            </div>
          </div>
        ))}
      </RadioGroup>

      {usesJina && (
        <div className="space-y-2">
          <Label htmlFor="web-fetch-key">{t('admin.web-fetch.key')}</Label>
          <form
            className="flex gap-2"
            onSubmit={(event) => {
              event.preventDefault()
              if (key.trim()) saveKey(key.trim())
            }}
          >
            <PasswordInput
              id="web-fetch-key"
              className="flex-1"
              autoComplete="off"
              placeholder={settings.jinaKeyPreview ?? 'jina_…'}
              value={key}
              disabled={update.isPending}
              onChange={(event) => setKey(event.target.value)}
            />
            <Button type="submit" disabled={!key.trim() || update.isPending}>
              {t('admin.web-search.save-key')}
            </Button>
            {settings.jinaKeySet && (
              <Button type="button" variant="outline" disabled={update.isPending} onClick={() => saveKey('')}>
                {t('admin.channels.api-key-clear')}
              </Button>
            )}
          </form>
          <p className="text-muted-foreground text-sm">
            {settings.jinaKeySet ? t('admin.web-fetch.key-stored') : t('admin.web-fetch.key-missing')}{' '}
            <a
              href="https://jina.ai/reader"
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
        <Label htmlFor="web-fetch-test">{t('admin.web-fetch.test')}</Label>
        <form
          className="flex gap-2"
          onSubmit={(event) => {
            event.preventDefault()
            if (address.trim()) test.mutate(address.trim())
          }}
        >
          <Input
            id="web-fetch-test"
            className="flex-1"
            placeholder="https://openai.com/index/introducing-chatgpt-search/"
            value={address}
            onChange={(event) => setAddress(event.target.value)}
          />
          <Button type="submit" variant="outline" disabled={test.isPending || !address.trim()}>
            {test.isPending && <Loader2 className="animate-spin" aria-hidden="true" />}
            {t('admin.web-fetch.test-run')}
          </Button>
        </form>
        {test.data &&
          (test.data.ok ? (
            <Alert>
              <CheckCircle2 aria-hidden="true" />
              <AlertTitle>
                {t('admin.web-fetch.test-ok', {
                  via: test.data.via === 'jina' ? 'Jina Reader' : t('admin.web-fetch.builtin'),
                  chars: test.data.chars ?? 0,
                  ms: test.data.latencyMs,
                })}
              </AlertTitle>
              <AlertDescription>
                {test.data.title && <p className="font-medium">{test.data.title}</p>}
                <p className="line-clamp-4 break-all">{test.data.preview}</p>
              </AlertDescription>
            </Alert>
          ) : (
            <Alert variant="destructive">
              <XCircle aria-hidden="true" />
              <AlertTitle>{t('admin.web-fetch.test-failed')}</AlertTitle>
              <AlertDescription>{test.data.error}</AlertDescription>
            </Alert>
          ))}
      </div>
    </div>
  )
}
