import { zodResolver } from '@hookform/resolvers/zod'
import type { SystemSettings } from '@openchat/api-client'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { z } from 'zod'

import { Button } from '@/components/shadcn/ui/button'
import { Form, FormControl, FormDescription, FormField, FormItem, FormLabel, FormMessage } from '@/components/shadcn/ui/form'
import { Input } from '@/components/shadcn/ui/input'
import { adminKeys } from '@/features/admin/use-admin'
import { api, unwrap } from '@/lib/api'

/**
 * How long the server's cleanup keeps what it removes: conversations people
 * deleted, uploads never sent, and files on the disk nothing points to. The
 * ranges match the server's (internal/api/retention.go), which checks them too.
 */
export function RetentionForm({ settings }: { settings: SystemSettings }) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()

  const whole = (min: number, max: number) =>
    z.coerce
      .number({ error: t('admin.settings.system.retention.whole') })
      .int(t('admin.settings.system.retention.whole'))
      .min(min, t('admin.settings.system.retention.range', { min, max }))
      .max(max, t('admin.settings.system.retention.range', { min, max }))
  const schema = z.object({
    deletedRetentionDays: whole(1, 3650),
    unsentUploadHours: whole(1, 8760),
    orphanFileHours: whole(1, 8760),
  })
  type Values = z.infer<typeof schema>

  const form = useForm<z.input<typeof schema>, unknown, Values>({
    resolver: zodResolver(schema),
    values: {
      deletedRetentionDays: settings.deletedRetentionDays,
      unsentUploadHours: settings.unsentUploadHours,
      orphanFileHours: settings.orphanFileHours,
    },
  })

  const save = useMutation({
    mutationFn: async (body: Values) => unwrap(await api.PATCH('/admin/system', { body })),
    onSuccess: (next) => {
      queryClient.setQueryData(adminKeys.system(), next)
      toast.success(t('admin.settings.system.retention.saved'))
    },
    onError: () => toast.error(t('admin.settings.system.save-failed')),
  })

  const fields = [
    { name: 'deletedRetentionDays', unit: 'days', label: 'deleted', hint: 'deleted-hint' },
    { name: 'unsentUploadHours', unit: 'hours', label: 'unsent', hint: 'unsent-hint' },
    { name: 'orphanFileHours', unit: 'hours', label: 'orphan', hint: 'orphan-hint' },
  ] as const

  return (
    <div className="space-y-4 py-4">
      <div className="space-y-1">
        <div className="text-sm font-medium">{t('admin.settings.system.retention.title')}</div>
        <p className="text-muted-foreground text-sm">{t('admin.settings.system.retention.hint')}</p>
      </div>
      <Form {...form}>
        <form className="space-y-4" onSubmit={form.handleSubmit((values) => save.mutate(values))}>
          {fields.map((item) => (
            <FormField
              key={item.name}
              control={form.control}
              name={item.name}
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t(`admin.settings.system.retention.${item.label}`)}</FormLabel>
                  <div className="flex items-center gap-2">
                    <FormControl>
                      <Input
                        type="number"
                        inputMode="numeric"
                        className="w-28"
                        {...field}
                        value={field.value as number | string}
                      />
                    </FormControl>
                    <span className="text-muted-foreground text-sm">{t(`admin.settings.system.retention.${item.unit}`)}</span>
                  </div>
                  <FormDescription>{t(`admin.settings.system.retention.${item.hint}`)}</FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />
          ))}
          <Button type="submit" disabled={save.isPending || !form.formState.isDirty}>
            {t('admin.settings.system.retention.save')}
          </Button>
        </form>
      </Form>
    </div>
  )
}
