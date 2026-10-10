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

/** The most the limits can be set to; the server's (internal/api/guest.go) checks them too. */
const MAX_HOURLY = 100_000
const MAX_DAILY = 10_000_000

/**
 * What a guest may ask of the model: requests per network address per hour, and
 * in all per day. Zero is no limit - the default - for both. A guest has no
 * account to count the requests against, so the address and the total are what
 * there is.
 */
export function GuestLimitsForm({ settings }: { settings: SystemSettings }) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()

  const whole = (max: number) =>
    z.coerce
      .number({ error: t('admin.settings.system.guest-limits.whole') })
      .int(t('admin.settings.system.guest-limits.whole'))
      .min(0, t('admin.settings.system.guest-limits.range', { min: 0, max }))
      .max(max, t('admin.settings.system.guest-limits.range', { min: 0, max }))
  const schema = z.object({ guestHourlyLimit: whole(MAX_HOURLY), guestDailyLimit: whole(MAX_DAILY) })
  type Values = z.infer<typeof schema>

  const form = useForm<z.input<typeof schema>, unknown, Values>({
    resolver: zodResolver(schema),
    values: { guestHourlyLimit: settings.guestHourlyLimit, guestDailyLimit: settings.guestDailyLimit },
  })

  const save = useMutation({
    mutationFn: async (body: Values) => unwrap(await api.PATCH('/admin/system', { body })),
    onSuccess: (next) => {
      queryClient.setQueryData(adminKeys.system(), next)
      toast.success(t('admin.settings.system.guest-limits.saved'))
    },
    onError: () => toast.error(t('admin.settings.system.save-failed')),
  })

  const fields = [
    { name: 'guestHourlyLimit', label: 'hourly', hint: 'hourly-hint' },
    { name: 'guestDailyLimit', label: 'daily', hint: 'daily-hint' },
  ] as const

  return (
    <div className="space-y-4 py-4">
      <div className="space-y-1">
        <div className="text-sm font-medium">{t('admin.settings.system.guest-limits.title')}</div>
        <p className="text-muted-foreground text-sm">{t('admin.settings.system.guest-limits.hint')}</p>
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
                  <FormLabel>{t(`admin.settings.system.guest-limits.${item.label}`)}</FormLabel>
                  <div className="flex items-center gap-2">
                    <FormControl>
                      <Input
                        type="number"
                        inputMode="numeric"
                        className="w-32"
                        {...field}
                        value={field.value as number | string}
                      />
                    </FormControl>
                    <span className="text-muted-foreground text-sm">{t('admin.settings.system.guest-limits.unit')}</span>
                  </div>
                  <FormDescription>{t(`admin.settings.system.guest-limits.${item.hint}`)}</FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />
          ))}
          <Button type="submit" disabled={save.isPending || !form.formState.isDirty}>
            {t('admin.settings.system.guest-limits.save')}
          </Button>
        </form>
      </Form>
    </div>
  )
}
