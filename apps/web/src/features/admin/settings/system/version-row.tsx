import type { VersionInfo } from '@openchat/api-client'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { ExternalLink, RefreshCw } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { Badge } from '@/components/shadcn/ui/badge'
import { Button } from '@/components/shadcn/ui/button'
import { Skeleton } from '@/components/shadcn/ui/skeleton'
import { adminKeys } from '@/features/admin/use-admin'
import { api, unwrap } from '@/lib/api'

/**
 * The version this server runs, and whether a newer one is out. Opening the
 * page reads what the server last saw (it looks again past six hours); the
 * button looks now. The server does the asking, so the browser never calls
 * GitHub and every administrator sees the same answer.
 */
export function VersionRow() {
  const { t, i18n } = useTranslation()
  const queryClient = useQueryClient()

  const info = useQuery({
    queryKey: adminKeys.version(),
    queryFn: async ({ signal }) => unwrap(await api.GET('/admin/version', { signal })),
    staleTime: 5 * 60_000,
  })
  const check = useMutation({
    mutationFn: async () => unwrap(await api.POST('/admin/version/check')),
    onSuccess: (next) => queryClient.setQueryData(adminKeys.version(), next),
  })

  const data = check.data ?? info.data
  return (
    <div className="flex items-start justify-between gap-6 py-4">
      <div className="min-w-0 space-y-1">
        <p className="text-sm font-medium">{t('admin.settings.system.version.label')}</p>
        {data ? <Status info={data} locale={i18n.language} /> : <Skeleton className="h-4 w-56" />}
      </div>
      <div className="flex shrink-0 items-center gap-2">
        {data && (
          <Badge variant="secondary" className="font-mono">
            {data.current === 'dev' ? t('admin.settings.system.version.dev') : `v${data.current}`}
          </Badge>
        )}
        <Button variant="outline" size="sm" disabled={check.isPending || info.isPending} onClick={() => check.mutate()}>
          <RefreshCw className={check.isPending ? 'animate-spin' : undefined} aria-hidden="true" />
          {check.isPending ? t('admin.settings.system.version.checking') : t('admin.settings.system.version.check')}
        </Button>
      </div>
    </div>
  )
}

function Status({ info, locale }: { info: VersionInfo; locale: string }) {
  const { t } = useTranslation()
  const when = (iso?: string) => (iso ? new Date(iso).toLocaleString(locale, { dateStyle: 'medium', timeStyle: 'short' }) : '')

  if (info.updateAvailable && info.latest) {
    return (
      <div className="space-y-1 text-sm">
        <p className="font-medium text-emerald-600 dark:text-emerald-400">
          {t('admin.settings.system.version.available', { version: info.latest })}
        </p>
        <p className="text-muted-foreground">
          {t('admin.settings.system.version.how')}{' '}
          {info.latestUrl && (
            <a
              href={info.latestUrl}
              target="_blank"
              rel="noreferrer"
              className="text-foreground inline-flex items-center gap-1 underline underline-offset-4"
            >
              {t('admin.settings.system.version.notes')}
              <ExternalLink className="size-3" aria-hidden="true" />
            </a>
          )}
        </p>
      </div>
    )
  }

  const lines: string[] = []
  if (info.current === 'dev') lines.push(t('admin.settings.system.version.dev-hint'))
  else if (info.latest) lines.push(t('admin.settings.system.version.current'))
  if (info.latest && info.current === 'dev') lines.push(t('admin.settings.system.version.latest-is', { version: info.latest }))
  if (info.checkError) lines.push(t(`admin.settings.system.version.error.${info.checkError}`))
  else if (info.checkedAt) lines.push(t('admin.settings.system.version.checked-at', { time: when(info.checkedAt) }))

  return <p className="text-muted-foreground text-sm">{lines.join(' ')}</p>
}
