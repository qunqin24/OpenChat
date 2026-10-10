import type { ReasoningEffort } from '@openchat/api-client'
import { zodResolver } from '@hookform/resolvers/zod'
import type { ReactNode } from 'react'
import { useForm, useWatch } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { z } from 'zod'

import { ModelIcon } from '@/components/model-icon'
import { Badge } from '@/components/shadcn/ui/badge'
import { Button } from '@/components/shadcn/ui/button'
import { Checkbox } from '@/components/shadcn/ui/checkbox'
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
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/shadcn/ui/select'
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetFooter,
  SheetHeader,
  SheetTitle,
} from '@/components/shadcn/ui/sheet'
import { Switch } from '@/components/shadcn/ui/switch'
import { PanelSection } from '../../channels/components/panel-section'
import {
  useResetModelSettings,
  useUpdateModelSettings,
  type CatalogModel,
  type InputModality,
  type ModelKind,
  type ModelOverride,
  type ModelSettings,
} from '../use-catalog'

const KINDS = [
  { value: 'text', label: 'admin.models.settings.kind-text' },
  { value: 'image', label: 'admin.models.settings.kind-image' },
  { value: 'embedding', label: 'admin.models.settings.kind-embedding' },
  { value: 'rerank', label: 'admin.models.settings.kind-rerank' },
] as const satisfies readonly { value: ModelKind; label: string }[]

const MODALITIES = [
  { value: 'image', label: 'admin.models.settings.modality-image' },
  { value: 'audio', label: 'admin.models.settings.modality-audio' },
  { value: 'video', label: 'admin.models.settings.modality-video' },
  { value: 'pdf', label: 'admin.models.settings.modality-pdf' },
] as const satisfies readonly { value: InputModality; label: string }[]

/** Lowest first, the order the chat picker lists them in. */
const EFFORTS = ['none', 'minimal', 'low', 'medium', 'high', 'xhigh', 'max'] as const satisfies readonly ReasoningEffort[]

type LimitField = 'contextLimit' | 'inputLimit' | 'outputLimit'

/** What a limit field suggests when the catalogue has no figure to show. */
const LIMIT_EXAMPLES: Record<LimitField, number> = {
  contextLimit: 128_000,
  inputLimit: 128_000,
  outputLimit: 16_000,
}

type ModelSettingsPanelProps = {
  model: CatalogModel
  open: boolean
  onOpenChange: (open: boolean) => void
}

/**
 * How one model is offered: what kind it is, what it may do, its token limits
 * and the thinking levels people may choose. Everything starts from the
 * catalogue's figures, and only what differs from them is saved — so a field
 * the operator never touched keeps following models.dev.
 *
 * Same shell/body split as the channel panel: the Sheet stays mounted so it
 * can animate out, and the form inside it mounts fresh on every opening.
 */
export function ModelSettingsPanel({ model, open, onOpenChange }: ModelSettingsPanelProps) {
  const { t } = useTranslation()
  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent className="w-full sm:max-w-lg">
        <SheetHeader className="text-start">
          <div className="flex items-center gap-3">
            <ModelIcon
              subject="model"
              providerId={model.providerId}
              family={model.family ?? model.modelId}
              name={model.providerName || model.name}
            />
            <div className="min-w-0">
              <SheetTitle className="truncate">{model.name}</SheetTitle>
              <SheetDescription className="truncate">{model.modelId}</SheetDescription>
            </div>
          </div>
          <p className="text-muted-foreground pt-1 text-sm">{t('admin.models.settings.subtitle')}</p>
        </SheetHeader>
        <SettingsForm model={model} onOpenChange={onOpenChange} />
      </SheetContent>
    </Sheet>
  )
}

/** The catalogue's figures; a model listed without them has nothing to fall back on. */
function defaultsOf(model: CatalogModel): ModelSettings {
  return (
    model.defaults ?? {
      kind: 'text',
      reasoning: false,
      toolCall: true,
      inputModalities: [],
      contextLimit: 0,
      inputLimit: 0,
      outputLimit: 0,
      reasoningEfforts: [],
    }
  )
}

function sameSet<T>(a: readonly T[], b: readonly T[]) {
  return a.length === b.length && a.every((item) => b.includes(item))
}

function SettingsForm({ model, onOpenChange }: Omit<ModelSettingsPanelProps, 'open'>) {
  const { t } = useTranslation()
  const update = useUpdateModelSettings()
  const reset = useResetModelSettings()
  const defaults = defaultsOf(model)
  const settings = model.settings ?? defaults
  const override = model.override ?? {}

  // A limit is text while it is being typed; empty means "the catalogue's".
  const limit = z
    .string()
    .trim()
    .refine((value) => value === '' || (/^\d+$/.test(value) && Number(value) > 0), {
      message: t('admin.models.settings.limit-invalid'),
    })
  const schema = z.object({
    kind: z.enum(['text', 'image', 'embedding', 'rerank']),
    reasoning: z.boolean(),
    toolCall: z.boolean(),
    inputModalities: z.array(z.enum(['image', 'audio', 'video', 'pdf'])),
    contextLimit: limit,
    inputLimit: limit,
    outputLimit: limit,
    reasoningEfforts: z.array(z.enum(EFFORTS)),
  })
  type Values = z.infer<typeof schema>

  const form = useForm<Values>({
    resolver: zodResolver(schema),
    defaultValues: {
      kind: settings.kind,
      reasoning: settings.reasoning,
      toolCall: settings.toolCall,
      inputModalities: settings.inputModalities,
      contextLimit: override.contextLimit ? String(override.contextLimit) : '',
      inputLimit: override.inputLimit ? String(override.inputLimit) : '',
      outputLimit: override.outputLimit ? String(override.outputLimit) : '',
      reasoningEfforts: settings.reasoningEfforts.filter((e) => e !== 'auto') as Values['reasoningEfforts'],
    },
  })

  const reasoning = useWatch({ control: form.control, name: 'reasoning' })
  const toolCall = useWatch({ control: form.control, name: 'toolCall' })

  // A model the catalogue gives no levels can still be vouched for: the
  // operator picks from every level, and they are sent as efforts.
  const nativeLevels = defaults.reasoningEfforts.filter((e) => e !== 'auto')
  const levelChoices = nativeLevels.length > 0 ? nativeLevels : [...EFFORTS]

  const customized = Object.values(override).some((value) => value !== undefined && value !== null)
  const pending = update.isPending || reset.isPending

  // Only what differs from the catalogue is sent; everything else is null, so
  // it goes on following models.dev.
  const toOverride = (values: Values): ModelOverride => {
    const limitOf = (field: LimitField) => {
      const raw = values[field].trim()
      if (raw === '') return null
      const value = Number(raw)
      return value === defaults[field] ? null : value
    }
    const efforts = EFFORTS.filter((e) => values.reasoningEfforts.includes(e))
    return {
      kind: values.kind === defaults.kind ? null : values.kind,
      reasoning: values.reasoning === defaults.reasoning ? null : values.reasoning,
      toolCall: values.toolCall === defaults.toolCall ? null : values.toolCall,
      inputModalities: sameSet(values.inputModalities, defaults.inputModalities)
        ? null
        : values.inputModalities,
      contextLimit: limitOf('contextLimit'),
      inputLimit: limitOf('inputLimit'),
      outputLimit: limitOf('outputLimit'),
      reasoningEfforts: sameSet(efforts, nativeLevels) ? null : efforts,
    }
  }

  const submit = async (values: Values) => {
    try {
      await update.mutateAsync({ modelId: model.modelId, override: toOverride(values) })
      toast.success(t('admin.models.settings.saved'))
      onOpenChange(false)
    } catch {
      toast.error(t('admin.models.settings.save-failed'))
    }
  }

  const restore = async () => {
    try {
      await reset.mutateAsync(model.modelId)
      toast.success(t('admin.models.settings.reset-done'))
      onOpenChange(false)
    } catch {
      toast.error(t('admin.models.settings.save-failed'))
    }
  }

  // A capability switch, in the bordered row the settings pages use.
  const switchRow = (name: 'reasoning' | 'toolCall', label: string, hint: string, modified: boolean) => (
    <FormField
      control={form.control}
      name={name}
      render={({ field }) => (
        <FormItem className="flex flex-row items-center justify-between gap-4 rounded-lg border p-3">
          <div className="space-y-0.5">
            <FieldLabel modified={modified}>{label}</FieldLabel>
            <FormDescription className="text-xs">{hint}</FormDescription>
          </div>
          <FormControl>
            <Switch checked={field.value} onCheckedChange={field.onChange} />
          </FormControl>
        </FormItem>
      )}
    />
  )

  const limitField = (field: LimitField, label: string, hint?: string) => (
    <FormField
      control={form.control}
      name={field}
      render={({ field: input }) => (
        <FormItem>
          <FieldLabel modified={input.value.trim() !== '' && Number(input.value) !== defaults[field]}>
            {label}
          </FieldLabel>
          <FormControl>
            <Input
              inputMode="numeric"
              placeholder={
                defaults[field] > 0
                  ? String(defaults[field])
                  : t('admin.models.settings.example', { value: LIMIT_EXAMPLES[field] })
              }
              {...input}
            />
          </FormControl>
          <FormDescription>
            {defaults[field] > 0
              ? t('admin.models.settings.default-value', { value: defaults[field].toLocaleString() })
              : t('admin.models.settings.no-default')}
            {hint ? ` ${hint}` : ''}
          </FormDescription>
          <FormMessage />
        </FormItem>
      )}
    />
  )

  return (
    <>
      <div className="flex flex-1 flex-col gap-6 overflow-y-auto px-4 py-1">
        <Form {...form}>
          <form
            id="model-settings-form"
            className="flex flex-col gap-8 pb-4"
            onSubmit={form.handleSubmit((values) => void submit(values))}
          >
            <PanelSection title={t('admin.models.settings.section-usage')}>
              <FormField
                control={form.control}
                name="kind"
                render={({ field }) => (
                  <FormItem>
                    <FieldLabel modified={field.value !== defaults.kind}>
                      {t('admin.models.settings.kind')}
                    </FieldLabel>
                    <Select value={field.value} onValueChange={field.onChange}>
                      <FormControl>
                        <SelectTrigger className="w-[200px]">
                          <SelectValue />
                        </SelectTrigger>
                      </FormControl>
                      <SelectContent>
                        {KINDS.map((option) => (
                          <SelectItem key={option.value} value={option.value}>
                            {t(option.label)}
                          </SelectItem>
                        ))}
                      </SelectContent>
                    </Select>
                    <FormDescription>{t('admin.models.settings.kind-hint')}</FormDescription>
                  </FormItem>
                )}
              />

              <div className="space-y-3">
                {switchRow(
                  'reasoning',
                  t('admin.models.settings.reasoning'),
                  t('admin.models.settings.reasoning-hint'),
                  reasoning !== defaults.reasoning,
                )}
                {switchRow(
                  'toolCall',
                  t('admin.models.settings.tool-call'),
                  t('admin.models.settings.tool-hint'),
                  toolCall !== defaults.toolCall,
                )}
              </div>

              <FormField
                control={form.control}
                name="inputModalities"
                render={({ field }) => (
                  <FormItem>
                    <FieldLabel modified={!sameSet(field.value, defaults.inputModalities)}>
                      {t('admin.models.settings.modalities')}
                    </FieldLabel>
                    <FormDescription>{t('admin.models.settings.modalities-hint')}</FormDescription>
                    <div className="grid grid-cols-2 gap-3 pt-1">
                      {MODALITIES.map((option) => (
                        <CheckboxItem
                          key={option.value}
                          label={t(option.label)}
                          checked={field.value.includes(option.value)}
                          onCheckedChange={(checked) =>
                            field.onChange(
                              checked
                                ? [...field.value, option.value]
                                : field.value.filter((m) => m !== option.value),
                            )
                          }
                        />
                      ))}
                    </div>
                  </FormItem>
                )}
              />
            </PanelSection>

            <PanelSection title={t('admin.models.settings.section-thinking')}>
              {reasoning ? (
                <FormField
                  control={form.control}
                  name="reasoningEfforts"
                  render={({ field }) => (
                    <FormItem>
                      <FieldLabel
                        modified={!sameSet(field.value, nativeLevels)}
                        action={
                          <div className="flex gap-1">
                            <Button
                              type="button"
                              variant="link"
                              size="sm"
                              className="h-auto px-1"
                              onClick={() => field.onChange([...levelChoices])}
                            >
                              {t('admin.models.settings.select-all')}
                            </Button>
                            <Button
                              type="button"
                              variant="link"
                              size="sm"
                              className="h-auto px-1"
                              onClick={() => field.onChange([])}
                            >
                              {t('admin.models.settings.select-none')}
                            </Button>
                          </div>
                        }
                      >
                        {t('admin.models.settings.efforts')}
                      </FieldLabel>
                      <FormDescription>
                        {nativeLevels.length > 0
                          ? t('admin.models.settings.efforts-hint')
                          : t('admin.models.settings.efforts-unknown')}
                      </FormDescription>
                      <div className="grid grid-cols-2 gap-3 pt-1 sm:grid-cols-3">
                        {levelChoices.map((level) => (
                          <CheckboxItem
                            key={level}
                            label={t(`chat.thinking.${level}`)}
                            checked={field.value.includes(level)}
                            onCheckedChange={(checked) =>
                              field.onChange(
                                checked ? [...field.value, level] : field.value.filter((e) => e !== level),
                              )
                            }
                          />
                        ))}
                      </div>
                    </FormItem>
                  )}
                />
              ) : (
                <p className="text-muted-foreground text-sm">{t('admin.models.settings.efforts-off')}</p>
              )}
            </PanelSection>

            <PanelSection title={t('admin.models.settings.section-limits')}>
              {limitField('contextLimit', t('admin.models.settings.context'))}
              {limitField('inputLimit', t('admin.models.settings.input-limit'))}
              {limitField(
                'outputLimit',
                t('admin.models.settings.output-limit'),
                t('admin.models.settings.output-hint'),
              )}
            </PanelSection>
          </form>
        </Form>
      </div>

      <SheetFooter className="flex-row items-center justify-between gap-2">
        <Button variant="ghost" disabled={pending || !customized} onClick={() => void restore()}>
          {t('admin.models.settings.reset')}
        </Button>
        <div className="flex gap-2">
          <Button variant="outline" disabled={pending} onClick={() => onOpenChange(false)}>
            {t('admin.ui.cancel')}
          </Button>
          <Button type="submit" form="model-settings-form" disabled={pending}>
            {t('admin.ui.save')}
          </Button>
        </div>
      </SheetFooter>
    </>
  )
}

/** A field's label, marked when the field no longer follows the catalogue. */
function FieldLabel({
  modified,
  action,
  children,
}: {
  modified: boolean
  action?: ReactNode
  children: ReactNode
}) {
  const { t } = useTranslation()
  return (
    <div className="flex items-center justify-between gap-2">
      <FormLabel className="gap-2">
        {children}
        {modified && (
          <Badge variant="secondary" className="px-1.5 py-0 text-[11px] font-normal">
            {t('admin.models.settings.modified')}
          </Badge>
        )}
      </FormLabel>
      {action}
    </div>
  )
}

function CheckboxItem({
  label,
  checked,
  onCheckedChange,
}: {
  label: string
  checked: boolean
  onCheckedChange: (checked: boolean) => void
}) {
  return (
    <label className="flex cursor-pointer items-center gap-2 text-sm font-normal">
      <Checkbox checked={checked} onCheckedChange={(value) => onCheckedChange(value === true)} />
      {label}
    </label>
  )
}
