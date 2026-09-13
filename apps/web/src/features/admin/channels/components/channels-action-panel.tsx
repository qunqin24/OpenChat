import type { Channel } from '@openchat/api-client'
import { zodResolver } from '@hookform/resolvers/zod'
import { ExternalLink } from 'lucide-react'
import { useState, type ReactNode } from 'react'
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
import { Separator } from '@/components/shadcn/ui/separator'
import { Switch } from '@/components/shadcn/ui/switch'
import { useCreateChannel, useUpdateChannel } from '@/features/admin/use-admin'
import { ProviderPicker } from './provider-picker'
import { useProviderCatalog } from '../use-provider-catalog'

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
 * A labelled run of fields. A heading and a rule, nothing more — the panel is
 * 512px wide and a card inside it would spend most of that width on its own
 * border.
 */
function FormSection({ title, children }: { title: string; children: ReactNode }) {
  return (
    <section className="space-y-4">
      <div className="space-y-2">
        <h3 className="text-sm leading-none font-medium">{title}</h3>
        <Separator />
      </div>
      {children}
    </section>
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
 *
 * The fields stack — label above control — rather than sitting in the label /
 * control columns the rest of the admin panel uses. Those columns are sized for
 * a dialog; at 512px minus the panel's padding they left the controls too
 * narrow to hold a URL, a provider name next to its model count, or a key field
 * with a "clear" button beside it, and the hints underneath were reduced to two
 * words a line. Stacking gives every control the full width.
 */
function ChannelForm({
  channel,
  onOpenChange,
}: Pick<ChannelsActionPanelProps, 'channel' | 'onOpenChange'>) {
  const { t } = useTranslation()
  const createChannel = useCreateChannel()
  const updateChannel = useUpdateChannel()
  const catalog = useProviderCatalog()
  const isEdit = channel !== undefined

  // Built here rather than at module scope so any message it carries is
  // translated. Name and provider are required, but the submit button below is
  // disabled while either is blank, so those messages are a backstop that the
  // form does not rely on to explain itself.
  const schema = z.object({
    name: z.string().trim().min(1).max(80),
    // A models.dev provider id rather than a label: it selects the catalogue
    // entry and the brand icon. The picker writes it, but anything the
    // catalogue does not list can still be typed in by hand.
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

  const providers = catalog.data ?? []
  // Undefined whenever the id was typed by hand or the catalogue is stale, in
  // which case the extra hints below simply do not appear. Nothing here depends
  // on the catalogue knowing the provider.
  const selected = providers.find((provider) => provider.id === providerId)
  // Upstream lists every variable a provider documents; the first is the one it
  // actually tells people to use.
  const envVar = selected?.env?.[0]

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
          className="flex-1 space-y-6 overflow-y-auto px-4 py-1"
          onSubmit={form.handleSubmit((values) => void submit(values))}
        >
          <FormSection title={t('admin.channels.section-connection')}>
            <FormField
              control={form.control}
              name="name"
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('admin.channels.name')}</FormLabel>
                  <FormControl>
                    <Input autoComplete="off" {...field} />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />

            <FormField
              control={form.control}
              name="providerId"
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('admin.channels.provider')}</FormLabel>
                  <FormControl>
                    <ProviderPicker
                      value={field.value}
                      onChange={field.onChange}
                      providers={providers}
                      isPending={catalog.isPending}
                      isError={catalog.isError}
                    />
                  </FormControl>
                  {selected && (
                    <FormDescription className="text-xs">
                      {t('admin.channels.models-available', { n: selected.modelCount })}
                    </FormDescription>
                  )}
                  <FormMessage />
                </FormItem>
              )}
            />

            <FormField
              control={form.control}
              name="baseUrl"
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('admin.channels.base-url')}</FormLabel>
                  <FormControl>
                    <Input
                      autoComplete="off"
                      spellCheck={false}
                      inputMode="url"
                      // Blank is a real answer here, not an unfilled field, so
                      // the placeholder says what blank will do.
                      placeholder={t('admin.channels.base-url-default')}
                      {...field}
                    />
                  </FormControl>
                  <FormDescription className="text-xs">
                    {t('admin.channels.base-url-hint')}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />
          </FormSection>

          <FormSection title={t('admin.channels.section-credential')}>
            <FormField
              control={form.control}
              name="apiKey"
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('admin.channels.api-key')}</FormLabel>
                  <FormControl>
                    <div className="flex items-center gap-2">
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
                  {/* One description element, because `FormDescription` carries
                      the id the control's `aria-describedby` points at and two
                      of them would be two elements with one id. The catalogue's
                      two facts about the credential — where the key normally
                      lives and where to go and read about it — belong next to
                      the field you cannot fill without them. */}
                  <FormDescription className="space-y-1 text-xs">
                    <span className="block">{t('admin.channels.api-key-hint')}</span>
                    {/* Not monospaced: the variable name is interpolated into a
                        sentence, and styling the sentence to suit the one token
                        inside it would set the surrounding prose — Chinese
                        included — in a code face. */}
                    {envVar && (
                      <span className="block">
                        {t('admin.channels.provider-env', { name: envVar })}
                      </span>
                    )}
                    {selected?.doc && (
                      <a
                        href={selected.doc}
                        // Leaves the app entirely, and `noreferrer` keeps the
                        // provider from learning where the click came from.
                        target="_blank"
                        rel="noreferrer"
                        className="text-foreground inline-flex items-center gap-1 underline underline-offset-2"
                      >
                        {t('admin.channels.provider-doc')}
                        <ExternalLink className="size-3" aria-hidden="true" />
                      </a>
                    )}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />

            <FormField
              control={form.control}
              name="enabled"
              render={({ field }) => (
                <FormItem className="flex flex-row items-center justify-between gap-4">
                  <FormLabel className="font-normal">{t('admin.channels.enabled')}</FormLabel>
                  <FormControl>
                    <Switch checked={field.value} onCheckedChange={field.onChange} />
                  </FormControl>
                </FormItem>
              )}
            />
          </FormSection>
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
