import { zodResolver } from '@hookform/resolvers/zod'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { z } from 'zod'

import { Button } from '@/components/shadcn/ui/button'
import { Form, FormControl, FormDescription, FormField, FormItem, FormLabel, FormMessage } from '@/components/shadcn/ui/form'
import { Input } from '@/components/shadcn/ui/input'
import { Skeleton } from '@/components/shadcn/ui/skeleton'
import { adminKeys } from '@/features/admin/use-admin'
import { api, unwrap } from '@/lib/api'

const GIB = 1024 ** 3

/**
 * How much each person may keep in files (internal/api/storage_quota.go): the
 * default for every account without a quota of its own, in GB. An account's
 * own is set on the Users page. Lowering it deletes nothing; uploads past it
 * are refused until the person makes room.
 */
export function QuotaForm() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const quota = useQuery({
    queryKey: adminKeys.storageQuota(),
    queryFn: async ({ signal }) => unwrap(await api.GET('/admin/storage/quota', { signal })),
  })

  const schema = z.object({
    gigabytes: z.coerce
      .number({ error: t('admin.storage.quota.invalid') })
      .min(0, t('admin.storage.quota.invalid'))
      .max(1_000_000, t('admin.storage.quota.invalid')),
  })
  type Values = z.infer<typeof schema>

  const form = useForm<z.input<typeof schema>, unknown, Values>({
    resolver: zodResolver(schema),
    values: quota.data ? { gigabytes: Number((quota.data.defaultBytes / GIB).toFixed(2)) } : undefined,
  })

  const save = useMutation({
    mutationFn: async (values: Values) =>
      unwrap(await api.PATCH('/admin/storage/quota', { body: { defaultBytes: Math.round(values.gigabytes * GIB) } })),
    onSuccess: (next) => {
      queryClient.setQueryData(adminKeys.storageQuota(), next)
      toast.success(t('admin.storage.quota.saved'))
    },
    onError: () => toast.error(t('admin.storage.quota.save-failed')),
  })

  return (
    <section className="space-y-6">
      <div>
        <h3 className="text-lg font-medium">{t('admin.storage.quota.title')}</h3>
        <p className="text-muted-foreground text-sm">{t('admin.storage.quota.hint')}</p>
      </div>
      {quota.isPending ? (
        <Skeleton className="h-20 w-full" />
      ) : (
        <Form {...form}>
          <form className="space-y-4" onSubmit={form.handleSubmit((values) => save.mutate(values))}>
            <FormField
              control={form.control}
              name="gigabytes"
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('admin.storage.quota.label')}</FormLabel>
                  <div className="flex items-center gap-2">
                    <FormControl>
                      <Input type="number" inputMode="decimal" step="0.5" min={0} className="w-32" {...field} value={field.value as number | string} />
                    </FormControl>
                    <span className="text-muted-foreground text-sm">GB</span>
                  </div>
                  <FormDescription>{t('admin.storage.quota.description')}</FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />
            <Button type="submit" disabled={save.isPending || !form.formState.isDirty}>
              {t('admin.storage.quota.save')}
            </Button>
          </form>
        </Form>
      )}
    </section>
  )
}
