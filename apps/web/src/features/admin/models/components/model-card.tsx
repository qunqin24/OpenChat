import { Braces, Brain, Paperclip, Wrench } from 'lucide-react'
import { useId } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { ModelIcon } from '@/components/model-icon'
import { Badge } from '@/components/shadcn/ui/badge'
import { Card } from '@/components/shadcn/ui/card'
import { Label } from '@/components/shadcn/ui/label'
import { Switch } from '@/components/shadcn/ui/switch'
import { formatCost, formatTokenLimit } from '../format'
import { useUpdateCatalogModel, type CatalogModel } from '../use-catalog'

/**
 * `as const` so the labels stay literal: i18next types `t()` against the
 * language files, and a widened `string` would not type-check.
 */
const CAPABILITIES = [
  { key: 'reasoning', label: 'admin.models.cap-reasoning', icon: Brain },
  { key: 'toolCall', label: 'admin.models.cap-tool', icon: Wrench },
  { key: 'attachment', label: 'admin.models.cap-attachment', icon: Paperclip },
  { key: 'structuredOutput', label: 'admin.models.cap-structured', icon: Braces },
] as const

function Stat({ label, value }: { label: string; value: string }) {
  return (
    <div className="min-w-0">
      <dt className="text-muted-foreground text-[11px] leading-tight">{label}</dt>
      <dd className="truncate text-xs font-medium tabular-nums">{value}</dd>
    </div>
  )
}

/**
 * One model in the marketplace. The card carries everything a decision needs —
 * who makes it, what it can do, what it costs — and the switch that acts on it,
 * so nothing has to be opened to choose.
 */
export function ModelCard({ model }: { model: CatalogModel }) {
  const { t } = useTranslation()
  const update = useUpdateCatalogModel()
  const switchId = useId()

  const capabilities = CAPABILITIES.filter((capability) => model[capability.key])

  const cost =
    model.inputCost === undefined && model.outputCost === undefined
      ? undefined
      : `${model.inputCost === undefined ? '—' : formatCost(model.inputCost)} / ${
          model.outputCost === undefined ? '—' : formatCost(model.outputCost)
        }`

  const toggle = (enabled: boolean) => {
    update.mutate(
      { providerId: model.providerId, modelId: model.modelId, enabled },
      // The server's own message is not localised, so it is never rendered; the
      // same refusal toast the user table uses says enough.
      { onError: () => toast.error(t('admin.users.update-failed')) },
    )
  }

  return (
    <Card className="h-full gap-0 overflow-hidden py-0">
      <div className="flex items-start gap-3 p-4">
        <ModelIcon
          providerId={model.providerId}
          family={model.family}
          name={model.providerName}
        />
        <div className="min-w-0 flex-1">
          {/* The id is what goes into a config, so it is worth having to hand
              even though the name is what the card shows. */}
          <p
            className="truncate text-sm leading-tight font-semibold"
            title={`${model.name} · ${model.modelId}`}
          >
            {model.name}
          </p>
          <p className="text-muted-foreground truncate text-xs" title={model.providerName}>
            {model.providerName}
          </p>
        </div>
      </div>

      {model.description && (
        <p className="text-muted-foreground line-clamp-2 px-4 text-xs leading-relaxed">
          {model.description}
        </p>
      )}

      {capabilities.length > 0 && (
        <div className="flex flex-wrap gap-1 px-4 pt-3">
          {capabilities.map((capability) => (
            <Badge key={capability.key} variant="secondary" className="font-normal">
              <capability.icon aria-hidden="true" />
              {t(capability.label)}
            </Badge>
          ))}
        </div>
      )}

      <dl className="mt-auto grid grid-cols-3 gap-3 px-4 pt-4">
        <Stat
          label={t('admin.models.context')}
          value={model.contextLimit === undefined ? '—' : formatTokenLimit(model.contextLimit)}
        />
        <Stat
          label={t('admin.models.output')}
          value={model.outputLimit === undefined ? '—' : formatTokenLimit(model.outputLimit)}
        />
        <Stat label={t('admin.models.cost')} value={cost ?? '—'} />
      </dl>

      <div className="mt-4 flex items-center justify-between gap-2 border-t px-4 py-3">
        <Label htmlFor={switchId} className="text-muted-foreground text-xs font-normal">
          {model.enabled ? t('admin.models.enabled') : t('admin.models.enable')}
        </Label>
        <Switch
          id={switchId}
          checked={model.enabled}
          disabled={update.isPending}
          onCheckedChange={toggle}
        />
      </div>
    </Card>
  )
}
