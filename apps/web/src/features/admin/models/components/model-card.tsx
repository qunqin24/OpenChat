import { Link } from '@tanstack/react-router'
import { Braces, Brain, ChevronRight, Paperclip, PlugZap, PowerOff, Wrench } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { ModelIcon } from '@/components/model-icon'
import { Badge } from '@/components/shadcn/ui/badge'
import { Button } from '@/components/shadcn/ui/button'
import { Card } from '@/components/shadcn/ui/card'
import { cn } from '@/lib/utils'
import { formatCost, formatTokenLimit } from '../format'
import { type CatalogModel } from '../use-catalog'

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

  const capabilities = CAPABILITIES.filter((capability) => model[capability.key])

  // Which of our channels serve this, read from their own model lists. Only the
  // full catalogue can turn up a model with none — under `scope=channels` a
  // model is on the page precisely because some channel serves it.
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
  const hasFigures =
    model.contextLimit !== undefined || model.outputLimit !== undefined || cost !== undefined

  return (
    <Card className="h-full gap-0 overflow-hidden py-0">
      <div className="flex items-start gap-3 p-4">
        {/* With no catalogue entry there is no provider and no family, and the
            model's own id is the best brand hint left: `qwen-max` on an unknown
            relay still earns the Qwen mark, and anything unrecognised lands on
            the designed letter rather than a broken image. */}
        <ModelIcon
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
          {/* Borrowed from the channel panel's wording rather than duplicated:
              "not in the catalogue" is the same fact stated in the same words,
              and it explains the quieter card below without sounding like a
              fault. */}
          <p
            className="text-muted-foreground truncate text-xs"
            title={model.providerName || model.modelId}
          >
            {model.providerName || t('admin.channels.models-unknown')}
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

      {hasFigures && (
        <dl className="grid grid-cols-3 gap-3 px-4 pt-4">
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
      )}

      {/* The channels are the caveat on the status below: a model whose only
          channel is switched off is not something this site can answer with,
          however green its own switch looks in the channel panel. */}
      <div className="mt-auto flex flex-wrap items-center gap-1.5 px-4 pt-4 text-xs">
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
                variant={channel.enabled ? 'secondary' : 'outline'}
                className={cn(
                  'max-w-full font-normal',
                  !channel.enabled && 'text-muted-foreground border-dashed',
                )}
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
              </Badge>
            ))}
          </>
        )}
      </div>

      {/* Where the switch used to be, and deliberately not a switch any more.
          Offering a model is a fact about a *channel's* model, so a control here
          would have to guess which of the channels above it meant — and would
          mean something different on a card with one channel than on a card with
          three. The state is stated, and the channel panel is where it changes.
          The colours are the panel's own tokens, so this reads the same in both
          themes without inventing a palette. */}
      <div className="mt-4 flex items-center justify-between gap-2 border-t px-4 py-3">
        <span className="inline-flex items-center gap-2 text-xs">
          <span
            aria-hidden="true"
            className={cn(
              'size-1.5 shrink-0 rounded-full',
              model.enabled ? 'bg-primary' : 'bg-muted-foreground/40',
            )}
          />
          <span className={cn(!model.enabled && 'text-muted-foreground')}>
            {/* No "not enabled" string exists in either language file, and the
                users table's "Disabled" is the same word for the same state. */}
            {model.enabled ? t('admin.models.enabled') : t('admin.users.disabled')}
          </span>
        </span>

        {!unserved && (
          <Button asChild variant="ghost" size="sm" className="-me-2 h-7 px-2 text-xs">
            <Link to="/admin/channels">
              {t('admin.nav.channels')}
              <ChevronRight aria-hidden="true" />
            </Link>
          </Button>
        )}
      </div>
    </Card>
  )
}
