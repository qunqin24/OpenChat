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

import { limitField, MAX_RUNS } from './run-limit-schema'

/**
 * How many answers one account may have being written at once, across all its
 * conversations. Each is a stream of requests to a model that costs money, so
 * the number is what bounds the spend of one person (or one script). Zero is
 * no limit; administrators are never held to it.
 */
export function RunLimitForm({ settings }: { settings: SystemSettings }) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()

  const schema = z.object({
    maxConcurrentRuns: limitField(MAX_RUNS, {
      required: t('admin.settings.system.run-limit.required'),
      whole: t('admin.settings.system.run-limit.whole'),
      range: (max) => t('admin.settings.system.run-limit.range', { max }),
    }),
  })
  type Values = z.infer<typeof schema>

  const form = useForm<z.input<typeof schema>, unknown, Values>({
    resolver: zodResolver(schema),
    values: { maxConcurrentRuns: settings.maxConcurrentRuns },
  })

  const save = useMutation({
    mutationFn: async (body: Values) => unwrap(await api.PATCH('/admin/system', { body })),
    onSuccess: (next) => {
      queryClient.setQueryData(adminKeys.system(), next)
      toast.success(t('admin.settings.system.run-limit.saved'))
    },
    onError: () => toast.error(t('admin.settings.system.save-failed')),
  })

  return (
    <div className="space-y-4 py-4">
      <Form {...form}>
        <form className="space-y-4" onSubmit={form.handleSubmit((values) => save.mutate(values))}>
          <FormField
            control={form.control}
            name="maxConcurrentRuns"
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('admin.settings.system.run-limit.label')}</FormLabel>
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
                  <span className="text-muted-foreground text-sm">{t('admin.settings.system.run-limit.unit')}</span>
                </div>
                <FormDescription>{t('admin.settings.system.run-limit.hint')}</FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />
          <Button type="submit" disabled={save.isPending || !form.formState.isDirty}>
            {t('admin.settings.system.run-limit.save')}
          </Button>
        </form>
      </Form>
    </div>
  )
}
