import { Link } from '@tanstack/react-router'
import {
  Braces,
  Brain,
  Paperclip,
  PlugZap,
  PowerOff,
  SlidersHorizontal,
  Wrench,
} from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { ModelIcon } from '@/components/model-icon'
import { Badge } from '@/components/shadcn/ui/badge'
import { Button } from '@/components/shadcn/ui/button'
import { Card } from '@/components/shadcn/ui/card'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/shadcn/ui/tooltip'
import { cn } from '@/lib/utils'
import { formatCost, formatTokenLimit } from '../format'
import { type CatalogModel } from '../use-catalog'
import { ModelSettingsPanel } from './model-settings-panel'

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
 * who makes it, what it can do, what it costs, which channels serve it — and
 * says whether this installation offers it.
 *
 * What a channel serves is the channel's own statement; models.dev is only the
 * decoration on top of it. So every piece of metadata here is optional, and a
 * model the catalogue has never heard of is an ordinary card with less on it,
 * never an error: the blurb, the capability badges and the figures are each
 * left out rather than filled with a placeholder that would read as damage.
 */
export function ModelCard({ model }: { model: CatalogModel }) {
  const { t } = useTranslation()
  const [settingsOpen, setSettingsOpen] = useState(false)

  // The card shows the model as it is offered: the operator's settings over the
  // catalogue's, as the server resolved them.
  const settings = model.settings
  const has = (key: (typeof CAPABILITIES)[number]['key']) =>
    settings && (key === 'reasoning' || key === 'toolCall') ? settings[key] : model[key]
  const capabilities = CAPABILITIES.filter((capability) => has(capability.key))
  const contextLimit = settings?.contextLimit || model.contextLimit
  const outputLimit = settings?.outputLimit || model.outputLimit
  const customized = Object.values(model.override ?? {}).some(
    (value) => value !== undefined && value !== null,
  )
  const kind = settings?.kind ?? 'text'

  // Only channels where the operator selected this model are included.
  const channels = model.channels ?? []
  const unserved = channels.length === 0

  const cost =
    model.inputCost === undefined && model.outputCost === undefined
      ? undefined
      : `${model.inputCost === undefined ? '—' : formatCost(model.inputCost)} / ${
          model.outputCost === undefined ? '—' : formatCost(model.outputCost)
        }`

  // Three dashes in a row are how a card looks broken. When upstream has said
  // nothing about any of the figures — which is every model it does not list —
  // the row is dropped instead, and the card simply ends sooner.
  const hasFigures = !!contextLimit || !!outputLimit || cost !== undefined

  return (
    <Card className="h-full gap-0 overflow-hidden py-0">
      <div className="flex items-start gap-3 p-4">
        {/* With no catalogue entry there is no provider and no family, and the
            model's own id is the best brand hint left: `qwen-max` on an unknown
            relay still earns the Qwen mark, and anything unrecognised lands on
            the designed letter rather than a broken image. The card is the
            model's, so its family's mark leads the provider's. */}
        <ModelIcon
          subject="model"
          providerId={model.providerId}
          family={model.family ?? model.modelId}
          name={model.providerName || model.name}
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
          <p
            className="text-muted-foreground truncate text-xs"
            title={model.providerName || model.modelId}
          >
            {model.modelId}
          </p>
        </div>
        {/* A state, deliberately not a switch: offering a model is a fact about
            a *channel's* model, so a control here would have to guess which of
            the channels below it meant. The channel panel is where it changes. */}
        <span
          className={cn(
            'inline-flex shrink-0 items-center gap-1.5 text-xs',
            model.enabled ? 'text-foreground' : 'text-muted-foreground',
          )}
        >
          <span
            aria-hidden="true"
            className={cn(
              'size-1.5 rounded-full',
              model.enabled ? 'bg-emerald-500' : 'bg-muted-foreground/40',
            )}
          />
          {/* No "not enabled" string exists in either language file, and the
              users table's "Disabled" is the same word for the same state. */}
          {model.enabled ? t('admin.models.enabled') : t('admin.users.disabled')}
        </span>
      </div>

      {model.description && (
        <p className="text-muted-foreground line-clamp-2 px-4 text-xs leading-relaxed">
          {model.description}
        </p>
      )}

      {(capabilities.length > 0 || customized || kind !== 'text') && (
        <div className="flex flex-wrap gap-1 px-4 pt-3">
          {customized && (
            <Badge variant="outline" className="border-primary/30 text-primary font-normal">
              <SlidersHorizontal aria-hidden="true" />
              {t('admin.models.settings.customized')}
            </Badge>
          )}
          {kind !== 'text' && (
            <Badge variant="outline" className="font-normal">
              {t(`admin.models.settings.kind-${kind}`)}
            </Badge>
          )}
          {capabilities.map((capability) => (
            <Badge key={capability.key} variant="secondary" className="font-normal">
              <capability.icon aria-hidden="true" />
              {t(capability.label)}
            </Badge>
          ))}
        </div>
      )}

      {hasFigures && (
        <dl className="grid grid-cols-3 gap-3 px-4 pt-4">
          <Stat
            label={t('admin.models.context')}
            value={contextLimit ? formatTokenLimit(contextLimit) : '—'}
          />
          <Stat
            label={t('admin.models.output')}
            value={outputLimit ? formatTokenLimit(outputLimit) : '—'}
          />
          <Stat label={t('admin.models.cost')} value={cost ?? '—'} />
        </dl>
      )}

      {/* The channels are the caveat on the status above: a model whose only
          channel is switched off is not something this site can answer with,
          however green its own switch looks in the channel panel. Each one
          leads to the channel panel, which is where offering a model changes —
          so the card needs no separate link there. */}
      <div className="mt-auto flex items-end justify-between gap-2 px-4 pt-4 pb-4">
        <div className="flex min-w-0 flex-wrap items-center gap-1.5 text-xs">
          {unserved ? (
            <span className="text-muted-foreground inline-flex items-center gap-1.5">
              <PlugZap className="size-3.5 shrink-0" aria-hidden="true" />
              {t('admin.models.unreachable')}
            </span>
          ) : (
            <>
              <span className="text-muted-foreground shrink-0">{t('admin.models.via')}</span>
              {channels.map((channel) => (
                <Badge
                  key={channel.id}
                  asChild
                  variant={channel.enabled ? 'secondary' : 'outline'}
                  className={cn(
                    'max-w-full font-normal',
                    !channel.enabled && 'text-muted-foreground border-dashed',
                  )}
                >
                  <Link
                    to="/admin/channels"
                    title={
                      channel.enabled
                        ? channel.name
                        : `${channel.name} · ${t('admin.models.channel-disabled')}`
                    }
                  >
                    {!channel.enabled && <PowerOff aria-hidden="true" />}
                    <span className="truncate">{channel.name}</span>
                    {!channel.enabled && (
                      <span className="shrink-0">· {t('admin.models.channel-disabled')}</span>
                    )}
                  </Link>
                </Badge>
              ))}
            </>
          )}
        </div>

        <Tooltip>
          <TooltipTrigger asChild>
            <Button
              variant="ghost"
              size="icon"
              className="text-muted-foreground -me-1.5 -mb-1 size-7 shrink-0"
              aria-label={t('admin.models.settings.open')}
              onClick={() => setSettingsOpen(true)}
            >
              <SlidersHorizontal aria-hidden="true" />
            </Button>
          </TooltipTrigger>
          <TooltipContent>{t('admin.models.settings.open')}</TooltipContent>
        </Tooltip>
      </div>

      <ModelSettingsPanel model={model} open={settingsOpen} onOpenChange={setSettingsOpen} />
    </Card>
  )
}
