import type { Channel } from '@openchat/api-client'
import { zodResolver } from '@hookform/resolvers/zod'
import { useState } from 'react'
import { useForm, useWatch } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { z } from 'zod'

import { PasswordInput } from '@/components/shadcn/password-input'
import { Button } from '@/components/shadcn/ui/button'
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetFooter,
  SheetHeader,
  SheetTitle,
} from '@/components/shadcn/ui/sheet'
import {
  Form,
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/shadcn/ui/form'
import { Input } from '@/components/shadcn/ui/input'
import { Switch } from '@/components/shadcn/ui/switch'
import { useCreateChannel, useUpdateChannel } from '@/features/admin/use-admin'

type ChannelsActionPanelProps = {
  /** Absent in create mode. */
  channel?: Channel
  open: boolean
  onOpenChange: (open: boolean) => void
}

/**
 * Both halves of the channel form: adding a connection and editing one.
 *
 * Split in two on purpose. The shell stays mounted whether or not the panel is
 * open, because Radix needs the element to still be there to animate it out —
 * removing it the moment `open` goes false is what leaves a panel that slides
 * in and then blinks away. The body below it holds the form, and Radix mounts
 * that only while the panel is open, so every opening starts from a clean form
 * without anything having to reset it.
 */
export function ChannelsActionPanel({ channel, open, onOpenChange }: ChannelsActionPanelProps) {
  const { t } = useTranslation()
  const isEdit = channel !== undefined

  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent className="w-full sm:max-w-lg">
        <SheetHeader className="text-start">
          <SheetTitle>{t(isEdit ? 'admin.channels.edit' : 'admin.channels.create')}</SheetTitle>
          <SheetDescription>{t('admin.channels.subtitle')}</SheetDescription>
        </SheetHeader>

        <ChannelForm channel={channel} onOpenChange={onOpenChange} />
      </SheetContent>
    </Sheet>
  )
}

/**
 * The fields and the footer. Mounted by Radix when the panel opens.
 *
 * The one rule that matters here is the API key. It is write-only — the server
 * returns `hasKey` and a `keyPreview` like `…4f2a` and never the key itself —
 * so the field starts empty in *both* modes and an untouched field means
 * "leave the stored credential alone". Expressing that means omitting `apiKey`
 * from the PATCH body: sending `''` would clear the key, which would turn
 * renaming a channel into silently breaking it.
 */
function ChannelForm({
  channel,
  onOpenChange,
}: Pick<ChannelsActionPanelProps, 'channel' | 'onOpenChange'>) {
  const { t } = useTranslation()
  const createChannel = useCreateChannel()
  const updateChannel = useUpdateChannel()
  const isEdit = channel !== undefined

  // Built here rather than at module scope so any message it carries is
  // translated. Name and provider are required, but the submit button below is
  // disabled while either is blank, so those messages are a backstop that the
  // form does not rely on to explain itself.
  const schema = z.object({
    name: z.string().trim().min(1).max(80),
    // A models.dev provider id rather than a label: it selects the catalogue
    // entry and the brand icon. A plain input for now; a picker comes later.
    providerId: z.string().trim().min(1).max(80),
    baseUrl: z.string().trim(),
    apiKey: z.string(),
    enabled: z.boolean(),
  })

  const form = useForm<z.input<typeof schema>>({
    resolver: zodResolver(schema),
    defaultValues: {
      name: channel?.name ?? '',
      providerId: channel?.providerId ?? '',
      baseUrl: channel?.baseUrl ?? '',
      // Never seeded from the channel — there is nothing to seed it with, and
      // an empty field is exactly what "keep the stored key" looks like.
      apiKey: '',
      enabled: channel?.enabled ?? true,
    },
  })

  const pending = createChannel.isPending || updateChannel.isPending

  // A blank field means "keep the stored key", which leaves no way to say
  // "remove it" — the two are the same empty string. This carries the second
  // meaning, so both stay expressible without a magic value in the input.
  const [clearKey, setClearKey] = useState(false)

  // useWatch rather than form.watch(): the latter returns a fresh function the
  // React compiler refuses to memoise around.
  const name = useWatch({ control: form.control, name: 'name' })
  const providerId = useWatch({ control: form.control, name: 'providerId' })

  const submit = async (values: z.output<typeof schema>) => {
    try {
      if (isEdit) {
        await updateChannel.mutateAsync({
          channelId: channel.id,
          // Only what actually moved, so a no-op save is a no-op request.
          ...(values.name !== channel.name ? { name: values.name } : {}),
          ...(values.providerId !== channel.providerId
            ? { providerId: values.providerId }
            : {}),
          ...(values.baseUrl !== (channel.baseUrl ?? '') ? { baseUrl: values.baseUrl } : {}),
          ...(values.enabled !== channel.enabled ? { enabled: values.enabled } : {}),
          // The load-bearing line: the property exists only when the admin
          // actually typed a key, or asked for the stored one to go. Blank and
          // untouched leaves it in place, because the field is absent rather
          // than empty.
          ...(clearKey ? { apiKey: '' } : values.apiKey ? { apiKey: values.apiKey } : {}),
        })
        toast.success(t('admin.channels.updated'))
      } else {
        await createChannel.mutateAsync({
          name: values.name,
          providerId: values.providerId,
          enabled: values.enabled,
          ...(values.baseUrl ? { baseUrl: values.baseUrl } : {}),
          ...(values.apiKey ? { apiKey: values.apiKey } : {}),
        })
        toast.success(t('admin.channels.created'))
      }
      onOpenChange(false)
    } catch {
      // Nothing the contract blames on a single input, so there is no field to
      // point at — a toast it is.
      toast.error(t('admin.users.update-failed'))
    }
  }

  return (
    <>
      <Form {...form}>
        <form
          id="channels-action-form"
          className="flex-1 space-y-4 overflow-y-auto px-4 py-1"
          onSubmit={form.handleSubmit((values) => void submit(values))}
        >
          <FormField
            control={form.control}
            name="name"
            render={({ field }) => (
              <FormItem className="grid grid-cols-6 items-center gap-x-4 gap-y-1 space-y-0">
                <FormLabel className="col-span-2">{t('admin.channels.name')}</FormLabel>
                <FormControl>
                  <Input className="col-span-4" autoComplete="off" {...field} />
                </FormControl>
                <FormMessage className="col-span-4 col-start-3" />
              </FormItem>
            )}
          />

          <FormField
            control={form.control}
            name="providerId"
            render={({ field }) => (
              <FormItem className="grid grid-cols-6 items-center gap-x-4 gap-y-1 space-y-0">
                <FormLabel className="col-span-2">{t('admin.channels.provider')}</FormLabel>
                <FormControl>
                  <Input
                    className="col-span-4"
                    autoComplete="off"
                    spellCheck={false}
                    placeholder="openai"
                    {...field}
                  />
                </FormControl>
                <FormMessage className="col-span-4 col-start-3" />
              </FormItem>
            )}
          />

          <FormField
            control={form.control}
            name="baseUrl"
            render={({ field }) => (
              <FormItem className="grid grid-cols-6 items-center gap-x-4 gap-y-1 space-y-0">
                <FormLabel className="col-span-2">{t('admin.channels.base-url')}</FormLabel>
                <FormControl>
                  <Input
                    className="col-span-4"
                    autoComplete="off"
                    spellCheck={false}
                    inputMode="url"
                    {...field}
                  />
                </FormControl>
                <FormDescription className="col-span-4 col-start-3 text-xs">
                  {t('admin.channels.base-url-hint')}
                </FormDescription>
                <FormMessage className="col-span-4 col-start-3" />
              </FormItem>
            )}
          />

          <FormField
            control={form.control}
            name="apiKey"
            render={({ field }) => (
              <FormItem className="grid grid-cols-6 items-center gap-x-4 gap-y-1 space-y-0">
                <FormLabel className="col-span-2">{t('admin.channels.api-key')}</FormLabel>
                <FormControl>
                  <div className="col-span-4 flex items-center gap-2">
                    <PasswordInput
                      className="flex-1"
                      autoComplete="off"
                      // The stored key can only ever be hinted at, so the
                      // preview stands in for a value the field cannot hold.
                      placeholder={
                        clearKey
                          ? t('admin.channels.api-key-clearing')
                          : (channel?.keyPreview ?? 'sk-…')
                      }
                      disabled={clearKey}
                      {...field}
                    />
                    {isEdit && channel.hasKey && (
                      <Button
                        type="button"
                        variant="ghost"
                        size="sm"
                        className="shrink-0"
                        onClick={() => {
                          setClearKey((current) => !current)
                          form.setValue('apiKey', '')
                        }}
                      >
                        {t(clearKey ? 'admin.ui.cancel' : 'admin.channels.api-key-clear')}
                      </Button>
                    )}
                  </div>
                </FormControl>
                <FormDescription className="col-span-4 col-start-3 text-xs">
                  {t('admin.channels.api-key-hint')}
                </FormDescription>
                <FormMessage className="col-span-4 col-start-3" />
              </FormItem>
            )}
          />

          <FormField
            control={form.control}
            name="enabled"
            render={({ field }) => (
              <FormItem className="grid grid-cols-6 items-center gap-x-4 gap-y-1 space-y-0">
                <FormLabel className="col-span-2">{t('admin.channels.enabled')}</FormLabel>
                <div className="col-span-4 flex items-center gap-3">
                  <FormControl>
                    <Switch checked={field.value} onCheckedChange={field.onChange} />
                  </FormControl>
                </div>
              </FormItem>
            )}
          />
        </form>
      </Form>

      <SheetFooter className="flex-row justify-end">
        <Button variant="outline" disabled={pending} onClick={() => onOpenChange(false)}>
          {t('admin.ui.cancel')}
        </Button>
        <Button
          type="submit"
          form="channels-action-form"
          // Keeps the two required fields from ever reaching zod, whose
          // built-in messages are not localised.
          disabled={pending || !name.trim() || !providerId.trim()}
        >
          {t('admin.ui.save')}
        </Button>
      </SheetFooter>
    </>
  )
}
