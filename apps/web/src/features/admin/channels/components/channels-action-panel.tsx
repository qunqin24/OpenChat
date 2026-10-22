import { OpenChatError, type Channel } from '@openchat/api-client'
import { zodResolver } from '@hookform/resolvers/zod'
import { ExternalLink } from 'lucide-react'
import { useEffect, useRef, useState } from 'react'
import { useForm, useWatch } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { z } from 'zod'

import { ModelIcon } from '@/components/model-icon'
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
import { Label } from '@/components/shadcn/ui/label'
import { RadioGroup, RadioGroupItem } from '@/components/shadcn/ui/radio-group'
import { Switch } from '@/components/shadcn/ui/switch'
import { useCreateChannel, useUpdateChannel } from '@/features/admin/use-admin'
import { ChannelProviderPicker } from './channel-provider-picker'
import { ChannelModelsSection } from './channel-models-section'
import { ChannelModelPicker } from './channel-model-picker'
import type { ChannelModelInput } from '../use-channel-models'
import { PanelSection } from './panel-section'
import { cn } from '@/lib/utils'
import { DEFAULT_CHANNEL_TYPE, useChannelTypes, type ChannelType } from '../channel-types'
import { defaultSendUserId } from '../send-user-id'

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
 *
 * The fields stack — label above control — rather than sitting in the label /
 * control columns the rest of the admin panel uses. Those columns are sized for
 * a dialog; at 512px minus the panel's padding they left the controls too
 * narrow to hold a URL, a provider name next to its model count, or a key field
 * with a "clear" button beside it, and the hints underneath were reduced to two
 * words a line. Stacking gives every control the full width.
 *
 * The second rule: one question, not two. The protocol and the brand used to be
 * separate fields, which asked the operator "which protocol does this speak" —
 * a question they should never have to answer. A type answers it: the server's
 * table maps each vendor to its protocol, its default endpoint and its key
 * format, so picking "OpenRouter" settles all three.
 *
 * Creating starts on the vendor picker rather than the fields: the vendor is
 * the one choice everything after it depends on, and a grid of logos with a
 * search box finds it faster than a dropdown of thirty names. The fields stay
 * mounted, only hidden, while the picker is up — so going back to change the
 * vendor never costs the operator what they had typed.
 */
function ChannelForm({
  channel,
  onOpenChange,
}: Pick<ChannelsActionPanelProps, 'channel' | 'onOpenChange'>) {
  const { t } = useTranslation()
  const createChannel = useCreateChannel()
  const updateChannel = useUpdateChannel()
  const channelTypes = useChannelTypes()
  const isEdit = channel !== undefined

  // Built here rather than at module scope so any message it carries is
  // translated. The name is the only text that is required, and the submit
  // button below is disabled while it is blank, so that message is a backstop
  // the form does not rely on to explain itself.
  const schema = z.object({
    name: z.string().trim().min(1).max(80),
    // Required, and the one field here the server acts on: it picks the adapter
    // that talks to the endpoint. Not an enum of our own — the server owns the
    // list, and a copy here would reject a type it had just started offering.
    // `custom` keeps the union from the contract without restating its members;
    // the picker only ever emits one of them, so this is a statement of what the
    // contract allows rather than a check anyone is expected to fail.
    type: z.custom<ChannelType>((value) => typeof value === 'string' && value.length > 0),
    baseUrl: z.string().trim(),
    apiKey: z.string(),
    enabled: z.boolean(),
    apiFormat: z.enum(['chat', 'responses']),
    // Typed as text and read as whole numbers (the server's bounds): a number
    // input's stepper and scroll-to-change are more trouble than help here.
    priority: z
      .string()
      .trim()
      .refine((v) => /^-?\d{1,7}$/.test(v) && Math.abs(Number(v)) <= 1_000_000, t('admin.channels.priority-invalid')),
    weight: z
      .string()
      .trim()
      .refine((v) => /^\d{1,4}$/.test(v) && Number(v) >= 1 && Number(v) <= 1000, t('admin.channels.weight-invalid')),
  })

  const form = useForm<z.input<typeof schema>>({
    resolver: zodResolver(schema),
    defaultValues: {
      name: channel?.name ?? '',
      type: channel?.type ?? DEFAULT_CHANNEL_TYPE,
      baseUrl: channel?.baseUrl ?? '',
      // Never seeded from the channel — there is nothing to seed it with, and
      // an empty field is exactly what "keep the stored key" looks like.
      apiKey: '',
      enabled: channel?.enabled ?? true,
      apiFormat: channel?.apiFormat ?? 'chat',
      priority: String(channel?.priority ?? 0),
      weight: String(channel?.weight ?? 1),
    },
  })

  // Which half of the sheet is showing. A new channel opens on the picker; an
  // existing one already has a vendor, so it opens on the fields and the picker
  // is reached through "Change".
  const [step, setStep] = useState<'picker' | 'form'>(isEdit ? 'form' : 'picker')
  // The default type above only keeps the form valid; it is not a choice. The
  // picker marks a vendor "current" and offers "back" only once one is chosen.
  const [chosen, setChosen] = useState(isEdit)
  const changeButton = useRef<HTMLButtonElement>(null)
  const refocusChange = useRef(false)

  // Coming back from the picker, focus goes to the control that opened it —
  // after the fields are visible again, which is why this is an effect and not
  // part of the handler: focusing an element that is still `display: none` does
  // nothing.
  useEffect(() => {
    if (step === 'form' && refocusChange.current) {
      refocusChange.current = false
      changeButton.current?.focus()
    }
  }, [step])

  const pending = createChannel.isPending || updateChannel.isPending

  // A blank field means "keep the stored key", which leaves no way to say
  // "remove it" — the two are the same empty string. This carries the second
  // meaning, so both stay expressible without a magic value in the input.
  const [clearKey, setClearKey] = useState(false)
  // The "send the user identifier" switch. A saved channel has its own answer;
  // a new one follows the endpoint (on only for OpenAI's and Anthropic's own)
  // until the operator touches it - and then it is sent, where an untouched
  // switch is left to the server's default, which is the same rule.
  const [sendUserChoice, setSendUserChoice] = useState<boolean | undefined>(channel?.sendUserId)
  const [selectedModels, setSelectedModels] = useState<ChannelModelInput[]>([])
  const [connectionRevision, setConnectionRevision] = useState(0)
  // Raised by the picker while a preview is in flight. Create waits on it so a
  // slow retry cannot be submitted with the selection the previous one left.
  const [modelsFetching, setModelsFetching] = useState(false)
  // A list fetched with the previous key or URL must never be submitted as a
  // selection for a different endpoint. Remounting aborts an old preview and
  // drops what it had selected.
  const resetModelSelection = () => {
    setSelectedModels([])
    setModelsFetching(false)
    setConnectionRevision((revision) => revision + 1)
  }

  // useWatch rather than form.watch(): the latter returns a fresh function the
  // React compiler refuses to memoise around.
  const name = useWatch({ control: form.control, name: 'name' })
  const selectedType = useWatch({ control: form.control, name: 'type' })
  const baseUrl = useWatch({ control: form.control, name: 'baseUrl' })
  const apiKey = useWatch({ control: form.control, name: 'apiKey' })

  const types = channelTypes.data ?? []
  // Undefined only while the table is still loading, in which case the hints
  // below simply do not appear yet.
  const selected = types.find((type) => type.id === selectedType)
  const sendUserId = sendUserChoice ?? defaultSendUserId(baseUrl.trim() || selected?.endpoint)

  // Choosing a vendor settles the type, discards the model list fetched for the
  // previous one, and names the channel after the vendor when the operator has
  // not named it themselves. "Not named it themselves" is a blank field or one
  // still holding the previous vendor's label — a name they typed is theirs.
  const chooseType = (next: ChannelType) => {
    if (next !== selectedType) {
      form.setValue('type', next, { shouldDirty: true })
      resetModelSelection()
    }
    const nextLabel = types.find((type) => type.id === next)?.label
    const previousLabel = chosen ? selected?.label : undefined
    const currentName = form.getValues('name').trim()
    if (nextLabel && (currentName === '' || currentName === previousLabel)) {
      form.setValue('name', nextLabel, { shouldDirty: true })
    }
    setChosen(true)
    refocusChange.current = true
    setStep('form')
  }

  const submit = async (values: z.output<typeof schema>) => {
    // Guarded here as well as on the footer button: a submit can arrive without
    // a click on it — Enter inside the form, or a programmatic call — and the
    // body must not run while a request it would race is still pending.
    if (pending || modelsFetching) return
    if (!isEdit && selectedModels.length === 0) return
    try {
      if (isEdit) {
        await updateChannel.mutateAsync({
          channelId: channel.id,
          // Only what actually moved, so a no-op save is a no-op request.
          ...(values.name !== channel.name ? { name: values.name } : {}),
          ...(values.type !== channel.type ? { type: values.type } : {}),
          // Compared against `?? ''` because the channel may carry no explicit
          // endpoint, and an empty string is how this field says "use the
          // vendor's default".
          ...(values.baseUrl !== (channel.baseUrl ?? '') ? { baseUrl: values.baseUrl } : {}),
          ...(values.enabled !== channel.enabled ? { enabled: values.enabled } : {}),
          ...(values.apiFormat !== channel.apiFormat ? { apiFormat: values.apiFormat } : {}),
          ...(sendUserId !== channel.sendUserId ? { sendUserId } : {}),
          ...(Number(values.priority) !== channel.priority ? { priority: Number(values.priority) } : {}),
          ...(Number(values.weight) !== channel.weight ? { weight: Number(values.weight) } : {}),
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
          type: values.type,
          enabled: values.enabled,
          models: selectedModels,
          priority: Number(values.priority),
          weight: Number(values.weight),
          // Only where the vendor offers the choice; elsewhere the server
          // ignores it, and sending it would only mislead a reader of the
          // request.
          ...(selected?.apiFormatChoice ? { apiFormat: values.apiFormat } : {}),
          ...(sendUserChoice !== undefined ? { sendUserId: sendUserChoice } : {}),
          ...(values.baseUrl ? { baseUrl: values.baseUrl } : {}),
          ...(values.apiKey ? { apiKey: values.apiKey } : {}),
        })
        toast.success(t('admin.channels.created'))
      }
      onOpenChange(false)
    } catch (error) {
      // Another channel already has this name: said on the field, which is
      // where it is fixed.
      if (error instanceof OpenChatError && error.code === 'channel_name_taken') {
        form.setError('name', { message: t('admin.channels.name-taken') })
        return
      }
      // `models_invalid` is the one failure the picker can be blamed for — it
      // is the contract's cap or a duplicate id. Everything else is a connection
      // or database fault with no field to point at.
      const key =
        error instanceof OpenChatError && error.code === 'models_invalid'
          ? 'admin.channels.models-invalid'
          : isEdit
            ? 'admin.channels.update-failed'
            : 'admin.channels.create-failed'
      toast.error(t(key))
    }
  }

  return (
    <>
      {step === 'picker' && (
        <ChannelProviderPicker
          types={types}
          isPending={channelTypes.isPending}
          current={chosen ? selectedType : undefined}
          onSelect={chooseType}
          onBack={
            chosen
              ? () => {
                  refocusChange.current = true
                  setStep('form')
                }
              : undefined
          }
        />
      )}

      {/* The scroll container holds both the form and the model list, but only
          the fields are inside the `<form>` element. The list behaves differently
          by mode — editing mutates a saved channel with a request per switch,
          creating keeps a draft picker — and in both cases nesting its inputs in
          the form would put a stray Enter one keystroke away from submitting the
          channel. */}
      <div
        className={cn(
          'flex flex-1 flex-col gap-6 overflow-y-auto px-4 py-1',
          // Hidden rather than unmounted, so the fields and the model selection
          // survive a trip to the picker.
          step === 'picker' && 'hidden',
        )}
      >
        <Form {...form}>
          <form
            id="channels-action-form"
            className="flex flex-col gap-6"
            onSubmit={form.handleSubmit((values) => void submit(values))}
          >
            <PanelSection title={t('admin.channels.section-connection')}>
              <FormField
                control={form.control}
                name="name"
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('admin.channels.name')}</FormLabel>
                    <FormControl>
                      <Input autoComplete="off" maxLength={80} {...field} />
                    </FormControl>
                    <FormMessage />
                  </FormItem>
                )}
              />

              {/* Not a FormField: there is nothing to type or validate, and a
                  label pointing at the button would replace its name with
                  "Type". The group is named instead. */}
              <div className="space-y-2" role="group" aria-labelledby="channels-type-label">
                <div id="channels-type-label" className="text-sm leading-none font-medium">
                  {t('admin.channels.type')}
                </div>
                <div className="flex items-center gap-3 rounded-lg border p-3">
                  <ModelIcon
                    brand={selected?.iconId}
                    name={selected?.label ?? selectedType}
                    className="size-8"
                  />
                  <span className="min-w-0 flex-1 truncate text-sm font-medium">
                    {selected?.label ?? selectedType}
                  </span>
                  <Button
                    ref={changeButton}
                    type="button"
                    variant="outline"
                    size="sm"
                    className="shrink-0"
                    onClick={() => setStep('picker')}
                  >
                    {t('admin.channels.type-change')}
                  </Button>
                </div>
              </div>

              {/* A vendor with separate sites (MiniMax's China and international
                  platforms): a key works on one of them only, so the operator
                  picks the one it is from. The first site is the type's own
                  endpoint and is chosen by leaving the base URL blank; a custom
                  address matches neither. */}
              {selected?.regions && selected.regions.length > 1 && (
                <FormItem>
                  <FormLabel>{t('admin.channels.region')}</FormLabel>
                  <RadioGroup
                    value={
                      (baseUrl.trim() === ''
                        ? selected.regions[0]
                        : selected.regions.find((region) => region.endpoint === baseUrl.trim())
                      )?.id ?? ''
                    }
                    onValueChange={(id) => {
                      const index = selected.regions!.findIndex((region) => region.id === id)
                      form.setValue('baseUrl', index <= 0 ? '' : selected.regions![index]!.endpoint, { shouldDirty: true })
                      resetModelSelection()
                    }}
                    className="grid grid-cols-2 gap-2"
                  >
                    {selected.regions.map((region) => (
                      <label
                        key={region.id}
                        className={cn(
                          'flex cursor-pointer items-start gap-3 rounded-md border p-3',
                          (baseUrl.trim() === '' ? selected.regions![0]!.id === region.id : region.endpoint === baseUrl.trim()) &&
                            'border-primary',
                        )}
                      >
                        <RadioGroupItem value={region.id} className="mt-0.5" />
                        <span className="grid gap-1">
                          <span className="text-sm font-medium">{t(`admin.channels.region-${region.id}`)}</span>
                          <code className="text-muted-foreground text-xs break-all">{new URL(region.endpoint).host}</code>
                        </span>
                      </label>
                    ))}
                  </RadioGroup>
                  <FormDescription className="text-xs">{t('admin.channels.region-hint')}</FormDescription>
                </FormItem>
              )}

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
                        // The type's own endpoint, so blank is not a mystery:
                        // the field shows what it will fall back to.
                        placeholder={selected?.endpoint || t('admin.channels.base-url-default')}
                        {...field}
                        onChange={(event) => {
                          field.onChange(event)
                          resetModelSelection()
                        }}
                      />
                    </FormControl>
                    <FormDescription className="text-xs">
                      {t('admin.channels.base-url-hint')}
                    </FormDescription>
                    <FormMessage />
                  </FormItem>
                )}
              />

              {/* Only on the OpenAI protocol, which has two request formats; the
                  others have one, and a gateway that routes per model (OpenCode
                  Go) chooses for itself. */}
              {selected?.apiFormatChoice && (
                <FormField
                  control={form.control}
                  name="apiFormat"
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>{t('admin.channels.api-format')}</FormLabel>
                      <FormControl>
                        <RadioGroup value={field.value} onValueChange={field.onChange} className="grid gap-2">
                          {(['chat', 'responses'] as const).map((format) => (
                            <label
                              key={format}
                              className={cn(
                                'flex cursor-pointer items-start gap-3 rounded-md border p-3',
                                field.value === format && 'border-primary',
                              )}
                            >
                              <RadioGroupItem value={format} className="mt-0.5" />
                              <span className="grid gap-1">
                                <span className="text-sm font-medium">
                                  {t(`admin.channels.api-format-${format}`)}
                                  <code className="text-muted-foreground ms-2 text-xs">
                                    {format === 'chat' ? '/chat/completions' : '/responses'}
                                  </code>
                                </span>
                                <span className="text-muted-foreground text-xs">
                                  {t(`admin.channels.api-format-${format}-hint`)}
                                </span>
                              </span>
                            </label>
                          ))}
                        </RadioGroup>
                      </FormControl>
                      <FormMessage />
                    </FormItem>
                  )}
                />
              )}
            </PanelSection>

            <PanelSection title={t('admin.channels.section-credential')}>
              <FormField
                control={form.control}
                name="apiKey"
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('admin.channels.api-key')}</FormLabel>
                    {/* FormControl wraps the input itself, not the row: RHF
                        injects the id and the `aria-*` that connect the label
                        and the error message onto its direct child, and those
                        belong on the field rather than on a layout div. The
                        Clear button sits beside the control, inside the row. */}
                    <div className="flex items-center gap-2">
                      <FormControl>
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
                          onChange={(event) => {
                            field.onChange(event)
                            resetModelSelection()
                          }}
                        />
                      </FormControl>
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
                    {/* One description element, because `FormDescription` carries
                        the id the control's `aria-describedby` points at and two
                        of them would be two elements with one id. The catalogue's
                        two facts about the credential — where the key normally
                        lives and where to go and read about it — belong next to
                        the field you cannot fill without them. */}
                    <FormDescription className="space-y-1 text-xs">
                      <span className="block">
                        {t(
                          isEdit
                            ? 'admin.channels.api-key-hint'
                            : 'admin.channels.api-key-create-hint',
                        )}
                      </span>
                      {/* The shape the vendor's keys take, which is how an
                          operator tells a pasted key from the wrong one. */}
                      {selected?.keyHint && (
                        <span className="block">
                          {t('admin.channels.api-key-format', { format: selected.keyHint })}
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
                          {t('admin.channels.api-key-doc')}
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

              {/* Not a form field: its default follows the endpoint above (see
                  sendUserChoice), which a field's static default cannot. */}
              <div className="flex flex-row items-start justify-between gap-4">
                <div className="space-y-1">
                  <Label htmlFor="channel-send-user-id" className="font-normal">
                    {t('admin.channels.send-user-id')}
                  </Label>
                  <p className="text-muted-foreground text-xs">{t('admin.channels.send-user-id-hint')}</p>
                </div>
                <Switch
                  id="channel-send-user-id"
                  className="mt-0.5 shrink-0"
                  checked={sendUserId}
                  onCheckedChange={setSendUserChoice}
                />
              </div>
            </PanelSection>

            <PanelSection title={t('admin.channels.section-routing')}>
              <p className="text-muted-foreground text-xs">{t('admin.channels.routing-hint')}</p>
              <div className="grid grid-cols-2 gap-4">
                <FormField
                  control={form.control}
                  name="priority"
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>{t('admin.channels.priority')}</FormLabel>
                      <FormControl>
                        <Input inputMode="numeric" autoComplete="off" {...field} />
                      </FormControl>
                      <FormDescription className="text-xs">{t('admin.channels.priority-hint')}</FormDescription>
                      <FormMessage />
                    </FormItem>
                  )}
                />
                <FormField
                  control={form.control}
                  name="weight"
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>{t('admin.channels.weight')}</FormLabel>
                      <FormControl>
                        <Input inputMode="numeric" autoComplete="off" {...field} />
                      </FormControl>
                      <FormDescription className="text-xs">{t('admin.channels.weight-hint')}</FormDescription>
                      <FormMessage />
                    </FormItem>
                  )}
                />
              </div>
            </PanelSection>
          </form>
        </Form>

        {isEdit ? (
          <ChannelModelsSection
            channel={channel}
            connectionDirty={
              selectedType !== channel.type ||
              baseUrl.trim() !== (channel.baseUrl ?? '') ||
              Boolean(apiKey) ||
              clearKey
            }
          />
        ) : (
          <ChannelModelPicker
            key={connectionRevision}
            connection={{ type: selectedType, baseUrl, apiKey }}
            selected={selectedModels}
            onChange={setSelectedModels}
            disabled={pending}
            onFetchingChange={setModelsFetching}
          />
        )}
      </div>

      {step === 'form' && (
        <SheetFooter className="flex-row justify-end">
          <Button variant="outline" disabled={pending} onClick={() => onOpenChange(false)}>
            {t('admin.ui.cancel')}
          </Button>
          <Button
            type="submit"
            form="channels-action-form"
            // Keeps the one required text field from ever reaching zod, whose
            // built-in messages are not localised. The type needs no such guard:
            // the picker always supplies a known one.
            //
            // `modelsFetching` overlaps `pending` only by accident — it is the
            // preview, not the create, that must block a submit carrying a stale
            // selection. The count then keeps a create with no models out of reach.
            disabled={
              pending ||
              modelsFetching ||
              !name.trim() ||
              (!isEdit && selectedModels.length === 0)
            }
          >
            {isEdit
              ? t('admin.ui.save')
              : t('admin.channels.create-with-models', { count: selectedModels.length })}
          </Button>
        </SheetFooter>
      )}
    </>
  )
}
