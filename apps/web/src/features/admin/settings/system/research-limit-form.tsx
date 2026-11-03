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

import { limitField, MAX_RESEARCH } from './run-limit-schema'

/**
 * How many deep researches one person may start per calendar month: each costs
 * an order of magnitude more than an answer. Zero is no limit; administrators
 * are never held to it.
 */
export function ResearchLimitForm({ settings }: { settings: SystemSettings }) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()

  const schema = z.object({
    deepResearchMonthly: limitField(MAX_RESEARCH, {
      required: t('admin.settings.system.research-limit.required'),
      whole: t('admin.settings.system.research-limit.whole'),
      range: (max) => t('admin.settings.system.research-limit.range', { max }),
    }),
  })
  type Values = z.infer<typeof schema>

  const form = useForm<z.input<typeof schema>, unknown, Values>({
    resolver: zodResolver(schema),
    values: { deepResearchMonthly: settings.deepResearchMonthly },
  })

  const save = useMutation({
    mutationFn: async (body: Values) => unwrap(await api.PATCH('/admin/system', { body })),
    onSuccess: (next) => {
      queryClient.setQueryData(adminKeys.system(), next)
      toast.success(t('admin.settings.system.research-limit.saved'))
    },
    onError: () => toast.error(t('admin.settings.system.save-failed')),
  })

  return (
    <div className="space-y-4 py-4">
      <Form {...form}>
        <form className="space-y-4" onSubmit={form.handleSubmit((values) => save.mutate(values))}>
          <FormField
            control={form.control}
            name="deepResearchMonthly"
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('admin.settings.system.research-limit.label')}</FormLabel>
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
                  <span className="text-muted-foreground text-sm">{t('admin.settings.system.research-limit.unit')}</span>
                </div>
                <FormDescription>{t('admin.settings.system.research-limit.hint')}</FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />
          <Button type="submit" disabled={save.isPending || !form.formState.isDirty}>
            {t('admin.settings.system.research-limit.save')}
          </Button>
        </form>
      </Form>
    </div>
  )
}
