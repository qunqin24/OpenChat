import { zodResolver } from '@hookform/resolvers/zod'
import { OpenChatError, type UpdateModerationSettingsRequest } from '@openchat/api-client'
import { useState } from 'react'
import { useForm, useWatch } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import type { z } from 'zod'

import { PasswordInput } from '@/components/shadcn/password-input'
import { Button } from '@/components/shadcn/ui/button'
import { Checkbox } from '@/components/shadcn/ui/checkbox'
import { Form, FormControl, FormDescription, FormField, FormItem, FormLabel, FormMessage } from '@/components/shadcn/ui/form'
import { Input } from '@/components/shadcn/ui/input'
import { RadioGroup, RadioGroupItem } from '@/components/shadcn/ui/radio-group'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/shadcn/ui/select'
import { Switch } from '@/components/shadcn/ui/switch'
import { useChatModels } from '@/hooks/use-chat-models'
import { cn } from '@/lib/utils'
import {
  DEFAULT_BLOCKED_CATEGORIES,
  MAX_BAN_HOURS,
  MAX_STRIKES,
  MAX_WINDOW_HOURS,
  MODERATION_CATEGORIES,
  categorySlug,
  moderationSchema,
  type ModerationMode,
} from './moderation-schema'
import { useUpdateModeration, type ModerationSettings } from './use-moderation'

const MODES: ModerationMode[] = ['off', 'openai', 'model']

/**
 * The content moderation settings: how a message is checked (not at all, by
 * OpenAI's moderation service, or by a model of the catalogue), what blocks it,
 * and what repeated offences cost the person. Saved together with one button;
 * the server checks every field again and answers 400 `invalid_moderation_*`.
 * The OpenAI key is write-only: the box is empty, the placeholder shows the
 * last four characters of the stored one, and a blank box keeps it.
 */
export function ModerationForm({ settings }: { settings: ModerationSettings }) {
  const { t } = useTranslation()
  const save = useUpdateModeration()
  const models = useChatModels()
  // "Clear the stored key" is a second meaning of the same empty box.
  const [clearKey, setClearKey] = useState(false)

  const schema = moderationSchema(
    {
      required: t('admin.moderation.form.required'),
      whole: t('admin.moderation.form.whole'),
      range: (min, max) => t('admin.moderation.form.range', { min, max }),
      score: t('admin.moderation.form.score'),
      url: t('admin.moderation.form.url'),
      keyRequired: t('admin.moderation.form.key-required'),
      modelRequired: t('admin.moderation.form.model-required'),
    },
    { keyStored: settings.openaiKeySet, clearingKey: clearKey },
  )
  type Values = z.output<typeof schema>

  const form = useForm<z.input<typeof schema>, unknown, Values>({
    resolver: zodResolver(schema),
    values: {
      mode: settings.mode,
      openaiApiKey: '',
      openaiBaseUrl: settings.openaiBaseUrl,
      model: settings.model,
      categories: settings.categories,
      minScore: settings.minScore,
      exemptAdmins: settings.exemptAdmins,
      onError: settings.onError,
      strikes: settings.strikes,
      strikeWindowHours: settings.strikeWindowHours,
      guestBanHours: settings.guestBanHours,
    },
  })
  const mode = useWatch({ control: form.control, name: 'mode' })
  const strikes = useWatch({ control: form.control, name: 'strikes' })

  const offered = models.data ?? []
  const storedModel = settings.model
  const stale = storedModel !== '' && !offered.some((m) => m.id === storedModel)

  const submit = (values: Values) => {
    const body: UpdateModerationSettingsRequest = {
      mode: values.mode,
      openaiBaseUrl: values.openaiBaseUrl,
      model: values.model,
      categories: values.categories,
      minScore: values.minScore,
      exemptAdmins: values.exemptAdmins,
      onError: values.onError,
      strikes: values.strikes,
      strikeWindowHours: values.strikeWindowHours,
      guestBanHours: values.guestBanHours,
      // Present only when the key is to change: absent keeps the stored one.
      ...(clearKey ? { openaiApiKey: '' } : values.openaiApiKey.trim() ? { openaiApiKey: values.openaiApiKey.trim() } : {}),
    }
    save.mutate(body, {
      onSuccess: () => {
        setClearKey(false)
        toast.success(t('admin.moderation.form.saved'))
      },
      onError: (error) => {
        const code = error instanceof OpenChatError ? error.code : ''
        toast.error(
          code === 'model_unavailable'
            ? t('admin.moderation.form.model-unavailable')
            : t('admin.settings.system.save-failed'),
        )
      },
    })
  }

  return (
    <Form {...form}>
      <form className="space-y-6" onSubmit={form.handleSubmit(submit)}>
        <FormField
          control={form.control}
          name="mode"
          render={({ field }) => (
            <FormItem>
              <FormLabel>{t('admin.moderation.form.mode')}</FormLabel>
              <FormControl>
                <RadioGroup value={field.value} onValueChange={field.onChange} className="grid gap-2">
                  {MODES.map((value) => (
                    <label
                      key={value}
                      className={cn(
                        'flex cursor-pointer items-start gap-3 rounded-md border p-3',
                        field.value === value && 'border-primary',
                      )}
                    >
                      <RadioGroupItem value={value} className="mt-0.5" />
                      <span className="grid gap-1">
                        <span className="text-sm font-medium">{t(`admin.moderation.mode.${value}`)}</span>
                        <span className="text-muted-foreground text-xs">{t(`admin.moderation.mode.${value}-hint`)}</span>
                      </span>
                    </label>
                  ))}
                </RadioGroup>
              </FormControl>
              <FormMessage />
            </FormItem>
          )}
        />

        {mode === 'openai' && (
          <div className="space-y-4">
            <FormField
              control={form.control}
              name="openaiApiKey"
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('admin.moderation.form.key')}</FormLabel>
                  <div className="flex items-center gap-2">
                    <FormControl>
                      <PasswordInput
                        className="flex-1"
                        autoComplete="off"
                        disabled={clearKey}
                        placeholder={
                          clearKey
                            ? t('admin.moderation.form.key-clearing')
                            : (settings.openaiKeyPreview ?? 'sk-…')
                        }
                        {...field}
                      />
                    </FormControl>
                    {settings.openaiKeySet && (
                      <Button
                        type="button"
                        variant="ghost"
                        size="sm"
                        className="shrink-0"
                        onClick={() => {
                          setClearKey((current) => !current)
                          form.setValue('openaiApiKey', '')
                        }}
                      >
                        {t(clearKey ? 'admin.ui.cancel' : 'admin.moderation.form.key-clear')}
                      </Button>
                    )}
                  </div>
                  <FormDescription className="text-xs">{t('admin.moderation.form.key-hint')}</FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />
            <FormField
              control={form.control}
              name="openaiBaseUrl"
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('admin.moderation.form.base-url')}</FormLabel>
                  <FormControl>
                    <Input
                      autoComplete="off"
                      spellCheck={false}
                      inputMode="url"
                      placeholder="https://api.openai.com/v1"
                      {...field}
                    />
                  </FormControl>
                  <FormDescription className="text-xs">{t('admin.moderation.form.base-url-hint')}</FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />
          </div>
        )}

        {mode === 'model' && (
          <FormField
            control={form.control}
            name="model"
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('admin.moderation.form.model')}</FormLabel>
                <Select value={field.value || undefined} onValueChange={field.onChange}>
                  <FormControl>
                    <SelectTrigger className="w-72 max-w-full">
                      <SelectValue placeholder={t('admin.moderation.form.model-placeholder')} />
                    </SelectTrigger>
                  </FormControl>
                  <SelectContent>
                    {stale && (
                      <SelectItem value={storedModel}>
                        {t('admin.settings.system.task-model-gone', { model: storedModel })}
                      </SelectItem>
                    )}
                    {offered.map((model) => (
                      <SelectItem key={model.id} value={model.id}>
                        {model.name}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
                <FormDescription className="text-xs">{t('admin.moderation.form.model-hint')}</FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />
        )}

        <FormField
          control={form.control}
          name="categories"
          render={({ field }) => (
            <FormItem>
              <div className="flex flex-wrap items-center justify-between gap-2">
                <FormLabel>{t('admin.moderation.form.categories')}</FormLabel>
                <Button
                  type="button"
                  variant="ghost"
                  size="sm"
                  onClick={() => form.setValue('categories', [...DEFAULT_BLOCKED_CATEGORIES], { shouldDirty: true })}
                >
                  {t('admin.moderation.form.categories-default')}
                </Button>
              </div>
              <FormDescription className="text-xs">{t('admin.moderation.form.categories-hint')}</FormDescription>
              <div className="grid gap-2 sm:grid-cols-2">
                {MODERATION_CATEGORIES.map((category) => {
                  const slug = categorySlug(category)
                  const on = field.value.includes(category)
                  return (
                    <label
                      key={category}
                      className={cn('flex cursor-pointer items-start gap-2 rounded-md border p-2.5', on && 'border-primary')}
                    >
                      <Checkbox
                        className="mt-0.5"
                        checked={on}
                        onCheckedChange={(checked) =>
                          field.onChange(checked ? [...field.value, category] : field.value.filter((c) => c !== category))
                        }
                      />
                      <span className="grid gap-0.5">
                        <span className="text-sm">{t(`admin.moderation.category.${slug}` as 'admin.moderation.category.sexual')}</span>
                        <code className="text-muted-foreground text-xs">{category}</code>
                      </span>
                    </label>
                  )
                })}
              </div>
              <FormMessage />
            </FormItem>
          )}
        />

        <FormField
          control={form.control}
          name="minScore"
          render={({ field }) => (
            <FormItem>
              <FormLabel>{t('admin.moderation.form.min-score')}</FormLabel>
              <FormControl>
                <Input type="number" inputMode="decimal" step="0.05" min={0} max={1} className="w-28" {...field} value={field.value as number | string} />
              </FormControl>
              <FormDescription className="text-xs">{t('admin.moderation.form.min-score-hint')}</FormDescription>
              <FormMessage />
            </FormItem>
          )}
        />

        <FormField
          control={form.control}
          name="exemptAdmins"
          render={({ field }) => (
            <FormItem className="flex flex-row items-start justify-between gap-6">
              <div className="space-y-1">
                <FormLabel>{t('admin.moderation.form.exempt-admins')}</FormLabel>
                <FormDescription className="text-xs">{t('admin.moderation.form.exempt-admins-hint')}</FormDescription>
              </div>
              <FormControl>
                <Switch className="mt-0.5 shrink-0" checked={field.value} onCheckedChange={field.onChange} />
              </FormControl>
            </FormItem>
          )}
        />

        <FormField
          control={form.control}
          name="onError"
          render={({ field }) => (
            <FormItem>
              <FormLabel>{t('admin.moderation.form.on-error')}</FormLabel>
              <Select value={field.value} onValueChange={field.onChange}>
                <FormControl>
                  <SelectTrigger className="w-56">
                    <SelectValue />
                  </SelectTrigger>
                </FormControl>
                <SelectContent>
                  <SelectItem value="allow">{t('admin.moderation.form.on-error-allow')}</SelectItem>
                  <SelectItem value="block">{t('admin.moderation.form.on-error-block')}</SelectItem>
                </SelectContent>
              </Select>
              <FormDescription className="text-xs">{t('admin.moderation.form.on-error-hint')}</FormDescription>
              <FormMessage />
            </FormItem>
          )}
        />

        <div className="space-y-4 rounded-md border p-4">
          <div className="space-y-1">
            <div className="text-sm font-medium">{t('admin.moderation.sanctions.title')}</div>
            <p className="text-muted-foreground text-sm">{t('admin.moderation.sanctions.hint')}</p>
          </div>
          {(
            [
              { name: 'strikes', unit: 'strikes-unit', max: MAX_STRIKES },
              { name: 'strikeWindowHours', unit: 'hours', max: MAX_WINDOW_HOURS },
              { name: 'guestBanHours', unit: 'hours', max: MAX_BAN_HOURS },
            ] as const
          ).map((item) => (
            <FormField
              key={item.name}
              control={form.control}
              name={item.name}
              render={({ field }) => (
                <FormItem className={cn(item.name !== 'strikes' && String(strikes).trim() === '0' && 'opacity-50')}>
                  <FormLabel>{t(`admin.moderation.sanctions.${item.name}`)}</FormLabel>
                  <div className="flex items-center gap-2">
                    <FormControl>
                      <Input type="number" inputMode="numeric" className="w-28" {...field} value={field.value as number | string} />
                    </FormControl>
                    <span className="text-muted-foreground text-sm">{t(`admin.moderation.sanctions.${item.unit}`)}</span>
                  </div>
                  <FormDescription className="text-xs">{t(`admin.moderation.sanctions.${item.name}-hint`)}</FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />
          ))}
        </div>

        <Button type="submit" disabled={save.isPending || (!form.formState.isDirty && !clearKey)}>
          {t('admin.moderation.form.save')}
        </Button>
      </form>
    </Form>
  )
}
