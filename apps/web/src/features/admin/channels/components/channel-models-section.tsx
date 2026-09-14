import type { Channel } from '@openchat/api-client'
import { Plus, RefreshCw, Search as SearchIcon } from 'lucide-react'
import { useMemo, useState, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Button } from '@/components/shadcn/ui/button'
import { Input } from '@/components/shadcn/ui/input'
import { Skeleton } from '@/components/shadcn/ui/skeleton'
import { cn } from '@/lib/utils'
import { ChannelModelRow } from './channel-model-row'
import { PanelSection } from './panel-section'
import {
  useAddChannelModel,
  useChannelModels,
  useRefreshChannelModels,
  useSetChannelModelsEnabled,
} from '../use-channel-models'

type ChannelModelsSectionProps = {
  /** A saved channel. A channel that does not exist yet cannot have models. */
  channel: Channel
}

/**
 * Which models this channel serves.
 *
 * The channel is the authority here, not the catalogue: the list comes from the
 * endpoint's own answer about itself, plus whatever an operator typed in for an
 * endpoint that publishes no list. models.dev only decorates what is already
 * there, so a model it has never heard of is normal and reads as normal.
 *
 * The whole list arrives in one response — around eighty models from OpenAI, a
 * few hundred from a relay — so the search box filters the array in place
 * rather than going back to the server, and the bulk buttons act on whatever
 * that filter is currently showing. The filter is the selection mechanism:
 * "enable all" after typing `gpt-4` is the reason to have either.
 */
export function ChannelModelsSection({ channel }: ChannelModelsSectionProps) {
  const { t } = useTranslation()
  const [search, setSearch] = useState('')
  const [draft, setDraft] = useState('')

  const models = useChannelModels(channel.id)
  const refresh = useRefreshChannelModels(channel.id)
  const addModel = useAddChannelModel(channel.id)
  const setEnabled = useSetChannelModelsEnabled(channel.id)

  const all = useMemo(() => models.data ?? [], [models.data])
  const query = search.trim().toLowerCase()
  const visible = useMemo(
    () =>
      query === ''
        ? all
        : all.filter(
            (model) =>
              model.modelId.toLowerCase().includes(query) ||
              model.displayName.toLowerCase().includes(query),
          ),
    [all, query],
  )

  const typed = draft.trim()
  // Adding an id the list already carries would be a 409 from the server; the
  // button simply stays out of reach instead, with the duplicate row visible
  // right there as the explanation.
  const duplicate = all.some((model) => model.modelId === typed)

  const fetchModels = async () => {
    try {
      const result = await refresh.mutateAsync()
      toast.success(
        t('admin.channels.models-fetched', {
          total: result.total,
          added: result.added,
          removed: result.removed,
        }),
      )
    } catch {
      // The two things that break this are a bad credential and a base URL that
      // is not an endpoint, and the operator is looking at both fields.
      toast.error(t('admin.channels.models-fetch-failed'))
    }
  }

  const add = async () => {
    if (typed === '' || duplicate) return
    try {
      await addModel.mutateAsync(typed)
      setDraft('')
    } catch {
      toast.error(t('admin.users.update-failed'))
    }
  }

  const setAll = async (enabled: boolean) => {
    const modelIds = visible
      .filter((model) => model.enabled !== enabled)
      .map((model) => model.modelId)
    if (modelIds.length === 0) return

    try {
      await setEnabled.mutateAsync({ modelIds, enabled })
    } catch {
      toast.error(t('admin.users.update-failed'))
    }
  }

  let list: ReactNode
  if (models.isError) {
    // The list is a separate request from the channel itself, so it can fail on
    // its own — the form above stays usable and says nothing about it.
    list = (
      <p className="text-muted-foreground rounded-md border border-dashed px-6 py-8 text-center text-sm">
        {t('state.error')}
      </p>
    )
  } else if (models.isPending) {
    list = (
      <div className="divide-y rounded-md border">
        {Array.from({ length: 3 }).map((_, index) => (
          <div key={index} className="px-3 py-2.5">
            <Skeleton className="h-8 w-full" />
          </div>
        ))}
      </div>
    )
  } else if (all.length === 0) {
    list = (
      <div className="flex flex-col items-center gap-1 rounded-md border border-dashed px-6 py-8 text-center">
        <p className="text-sm font-medium">{t('admin.channels.models-none')}</p>
        <p className="text-muted-foreground text-xs">{t('admin.channels.models-empty-hint')}</p>
      </div>
    )
  } else if (visible.length === 0) {
    // A filter that matches nothing is not an empty channel, and saying "no
    // models yet" here would send someone off to fetch a list they already have.
    list = (
      <p className="text-muted-foreground rounded-md border border-dashed px-6 py-8 text-center text-sm">
        {t('admin.ui.table.no-results')}
      </p>
    )
  } else {
    list = (
      // Capped and scrolled: a few hundred rows would otherwise push the add
      // field and the panel's footer out of reach.
      <div className="max-h-80 divide-y overflow-y-auto rounded-md border">
        {visible.map((model) => (
          <ChannelModelRow
            key={model.modelId}
            channelId={channel.id}
            model={model}
            disabled={setEnabled.isPending}
          />
        ))}
      </div>
    )
  }

  return (
    <PanelSection
      title={t('admin.channels.models')}
      action={
        <Button
          type="button"
          variant="outline"
          size="sm"
          // Refreshing calls the endpoint with the stored credential, so with no
          // key there is nothing to call it with. Saying that before the click
          // is kinder than a failed request that says the same thing later.
          disabled={!channel.hasKey || refresh.isPending}
          onClick={() => void fetchModels()}
        >
          {/* Under `prefers-reduced-motion` the icon stops turning, so the label
              carries the state instead: a frozen spinner and a hung panel look
              the same. */}
          <RefreshCw className={cn(refresh.isPending && 'animate-spin')} aria-hidden="true" />
          {t(refresh.isPending ? 'admin.channels.models-fetching' : 'admin.channels.models-fetch')}
        </Button>
      }
    >
      <div className="text-muted-foreground flex flex-wrap items-center justify-between gap-2 text-xs">
        {models.data && (
          <span className="tabular-nums">
            {t('admin.channels.models-count', { n: all.length })}
          </span>
        )}
        {/* Sits under the button it explains, rather than in a tooltip a
            disabled button cannot open. */}
        {!channel.hasKey && <span className="ms-auto">{t('admin.channels.models-need-key')}</span>}
      </div>

      {all.length > 0 && (
        <div className="flex flex-wrap items-center gap-2">
          <div className="relative min-w-40 flex-1">
            <SearchIcon
              className="text-muted-foreground pointer-events-none absolute start-2.5 top-1/2 size-4 -translate-y-1/2"
              aria-hidden="true"
            />
            <Input
              value={search}
              onChange={(event) => setSearch(event.target.value)}
              placeholder={t('admin.channels.models-search')}
              aria-label={t('admin.channels.models-search')}
              autoComplete="off"
              spellCheck={false}
              className="h-8 ps-8"
            />
          </div>

          <Button
            type="button"
            variant="outline"
            size="sm"
            disabled={setEnabled.isPending || !visible.some((model) => !model.enabled)}
            onClick={() => void setAll(true)}
          >
            {t('admin.channels.models-enable-all')}
          </Button>
          <Button
            type="button"
            variant="outline"
            size="sm"
            disabled={setEnabled.isPending || !visible.some((model) => model.enabled)}
            onClick={() => void setAll(false)}
          >
            {t('admin.channels.models-disable-all')}
          </Button>
        </div>
      )}

      {list}

      {/* Stays put in every state, empty list included — an endpoint that
          publishes no model list is exactly the case where this is the only way
          to get one. */}
      <div className="flex items-center gap-2">
        <Input
          value={draft}
          onChange={(event) => setDraft(event.target.value)}
          onKeyDown={(event) => {
            if (event.key === 'Enter') {
              event.preventDefault()
              void add()
            }
          }}
          placeholder={t('admin.channels.models-add-placeholder')}
          aria-label={t('admin.channels.models-add')}
          autoComplete="off"
          spellCheck={false}
          className="h-8 flex-1"
        />
        <Button
          type="button"
          variant="outline"
          size="sm"
          disabled={typed === '' || duplicate || addModel.isPending}
          onClick={() => void add()}
        >
          <Plus aria-hidden="true" />
          {t('admin.channels.models-add')}
        </Button>
      </div>
    </PanelSection>
  )
}
