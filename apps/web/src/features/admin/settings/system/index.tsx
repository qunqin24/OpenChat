import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { api, unwrap } from '@/lib/api'
import { adminKeys } from '@/features/admin/use-admin'
import { Label } from '@/components/shadcn/ui/label'
import { Skeleton } from '@/components/shadcn/ui/skeleton'
import { Switch } from '@/components/shadcn/ui/switch'
import { ContentSection } from '../components/content-section'

/**
 * Settings that belong to the server rather than to this browser — which is
 * what separates this section from Appearance next door.
 */
export function SettingsSystem() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()

  const { data, isPending } = useQuery({
    queryKey: adminKeys.system(),
    queryFn: async ({ signal }) => unwrap(await api.GET('/admin/system', { signal })),
  })

  const update = useMutation({
    mutationFn: async (debugLogging: boolean) =>
      unwrap(await api.PATCH('/admin/system', { body: { debugLogging } })),
    onSuccess: (next) => {
      queryClient.setQueryData(adminKeys.system(), next)
      toast.success(t(next.debugLogging ? 'admin.settings.system.on' : 'admin.settings.system.off'))
    },
    onError: () => toast.error(t('admin.users.update-failed')),
  })

  return (
    <ContentSection
      title={t('admin.settings.system.title')}
      desc={t('admin.settings.system.subtitle')}
    >
      <div className="flex items-start justify-between gap-6 rounded-lg border p-4">
        <div className="space-y-1">
          <Label htmlFor="debug-logging" className="text-base">
            {t('admin.settings.system.debug')}
          </Label>
          <p className="text-muted-foreground text-sm">
            {t('admin.settings.system.debug-hint')}
          </p>
        </div>

        {isPending ? (
          <Skeleton className="h-5 w-8 shrink-0" />
        ) : (
          <Switch
            id="debug-logging"
            className="mt-1 shrink-0"
            checked={data?.debugLogging ?? false}
            disabled={update.isPending}
            onCheckedChange={(value) => update.mutate(value)}
          />
        )}
      </div>
    </ContentSection>
  )
}

export default SettingsSystem
