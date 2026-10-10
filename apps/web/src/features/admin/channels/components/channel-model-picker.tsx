import { Plus, RefreshCw } from 'lucide-react'
import { useEffect, useId, useLayoutEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Alert, AlertDescription } from '@/components/shadcn/ui/alert'
import { Badge } from '@/components/shadcn/ui/badge'
import { Button } from '@/components/shadcn/ui/button'
import { Checkbox } from '@/components/shadcn/ui/checkbox'
import { Input } from '@/components/shadcn/ui/input'
import { Label } from '@/components/shadcn/ui/label'
import { cn } from '@/lib/utils'
import {
  channelModelErrorKey,
  previewChannelModels,
  type ChannelConnection,
  type ChannelModelInput,
} from '../use-channel-models'
import { PanelSection } from './panel-section'
import { useChannelTypes } from '../channel-types'

/**
 * The contract caps `CreateChannelRequest.models` at 2000 and the server
 * enforces the same number, so the picker stops here rather than let a large
 * relay build a selection the create call would answer with `models_invalid`.
 * Kept in step with `validateChannelModels`.
 */
const MAX_MODELS = 2000

type ChannelModelPickerProps = {
  connection: ChannelConnection
  selected: ChannelModelInput[]
  onChange: (models: ChannelModelInput[]) => void
  disabled: boolean
  /**
   * Reports an in-flight preview. The footer's Create button has to hold back
   * while one is running, or a slow retry would be submitted with whatever the
   * previous preview selected.
   */
  onFetchingChange?: (fetching: boolean) => void
}

/** A draft selection: fetching and checking boxes never save a channel. */
export function ChannelModelPicker({
  connection,
  selected,
  onChange,
  disabled,
  onFetchingChange,
}: ChannelModelPickerProps) {
  const { t } = useTranslation()
  const id = useId()
  const channelTypes = useChannelTypes()
  const controller = useRef<AbortController | null>(null)
  const [models, setModels] = useState<ChannelModelInput[]>([])
  const [search, setSearch] = useState('')
  const [manualId, setManualId] = useState('')
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<ReturnType<typeof channelModelErrorKey> | null>(null)

  // The parent mounts this afresh whenever the connection changes, so this
  // cleanup is what makes a stale preview impossible to apply to a new one.
  //
  // A layout effect, not a passive one: its cleanup runs synchronously while the
  // old picker is being removed. A passive cleanup is deferred, so a response
  // already in flight could resolve and call `onChange` first — repopulating a
  // selection for a connection the operator has just edited. Abort is the only
  // thing this effect does; the fetching flag below stays passive.
  useLayoutEffect(() => () => controller.current?.abort(), [])

  // Told, not inferred: the footer cannot see this component's fetch, and a
  // preview that lands after Create was pressed is worse than a disabled button.
  useEffect(() => {
    onFetchingChange?.(loading)
  }, [loading, onFetchingChange])

  const busy = disabled || loading
  // A preview calls the endpoint with a credential, which only a local service
  // (`keyOptional` in the served table) can go without; without a key the
  // button stays out of reach, matching the server's `channel_key_missing`.
  const keyOptional = channelTypes.data?.find((type) => type.id === connection.type)?.keyOptional
  const canFetch = Boolean(keyOptional) || Boolean(connection.apiKey?.trim())
  const query = search.trim().toLowerCase()
  const visible = models.filter((model) =>
    `${model.modelId} ${model.displayName}`.toLowerCase().includes(query),
  )
  const selectedIds = new Set(selected.map((model) => model.modelId))
  const typed = manualId.trim()
  const remaining = Math.max(0, MAX_MODELS - selected.length)
  const atCapacity = remaining === 0

  const fetchModels = async () => {
    controller.current?.abort()
    const request = new AbortController()
    controller.current = request
    setLoading(true)
    setError(null)
    try {
      const fetched = await previewChannelModels(connection, request.signal)
      if (request.signal.aborted) return
      // Hand-typed ids remain manual even if the endpoint also lists them, and
      // they survive a retry, so a model the endpoint stops advertising is not
      // quietly taken away from the operator who added it.
      const manual = models.filter((model) => model.source === 'manual')
      const manualIds = new Set(manual.map((model) => model.modelId))
      const next = [...manual, ...fetched.filter((model) => !manualIds.has(model.modelId))]
      setModels(next)
      // A selection the endpoint no longer offers is dropped with it: the
      // endpoint is the authority on what it serves.
      onChange(next.filter((model) => selectedIds.has(model.modelId)))
    } catch (err) {
      if (!request.signal.aborted) setError(channelModelErrorKey(err))
    } finally {
      if (!request.signal.aborted) setLoading(false)
    }
  }

  const toggle = (model: ChannelModelInput, checked: boolean) => {
    if (!checked) {
      onChange(selected.filter((item) => item.modelId !== model.modelId))
      return
    }
    if (atCapacity || selectedIds.has(model.modelId)) return
    onChange([...selected, model])
  }

  const selectVisible = (checked: boolean) => {
    if (!checked) {
      const visibleIds = new Set(visible.map((model) => model.modelId))
      onChange(selected.filter((model) => !visibleIds.has(model.modelId)))
      return
    }
    // Capped, so "select results" over a huge list stops at what create accepts
    // instead of building a payload the server will refuse.
    const additions = visible
      .filter((model) => !selectedIds.has(model.modelId))
      .slice(0, remaining)
    if (additions.length > 0) onChange([...selected, ...additions])
  }

  const addManual = () => {
    if (!typed || busy || atCapacity || models.some((model) => model.modelId === typed)) return
    const model: ChannelModelInput = { modelId: typed, displayName: '', source: 'manual' }
    setModels((current) => [...current, model])
    onChange([...selected, model])
    setManualId('')
    setSearch('')
  }

  return (
    <PanelSection
      title={t('admin.channels.models')}
      action={
        <Button
          type="button"
          variant="outline"
          size="sm"
          disabled={busy || !canFetch}
          onClick={() => void fetchModels()}
        >
          {/* Under `prefers-reduced-motion` the icon stops turning, so the label
              carries the state instead: a frozen spinner and a hung fetch look
              the same. */}
          <RefreshCw className={cn(loading && 'animate-spin')} aria-hidden="true" />
          {t(loading ? 'admin.channels.models-fetching' : 'admin.channels.models-fetch')}
        </Button>
      }
    >
      <p className="text-muted-foreground text-xs">{t('admin.channels.models-select-hint')}</p>
      {!canFetch && (
        <p className="text-muted-foreground text-xs">{t('admin.channels.models-enter-key')}</p>
      )}
      {error && (
        <Alert variant="destructive">
          <AlertDescription>{t(error)}</AlertDescription>
        </Alert>
      )}

      {models.length > 0 && (
        <>
          <Input
            value={search}
            onChange={(event) => setSearch(event.target.value)}
            placeholder={t('admin.channels.models-search')}
            aria-label={t('admin.channels.models-search')}
            autoComplete="off"
            spellCheck={false}
          />
          <div className="flex flex-wrap items-center gap-2">
            <span className="text-muted-foreground me-auto text-xs tabular-nums" role="status">
              {t('admin.channels.models-selected', {
                selected: selected.length,
                total: models.length,
              })}
            </span>
            <Button
              type="button"
              variant="ghost"
              size="sm"
              disabled={
                busy || atCapacity || visible.every((model) => selectedIds.has(model.modelId))
              }
              onClick={() => selectVisible(true)}
            >
              {t('admin.channels.models-select-all')}
            </Button>
            <Button
              type="button"
              variant="ghost"
              size="sm"
              disabled={busy || !visible.some((model) => selectedIds.has(model.modelId))}
              onClick={() => selectVisible(false)}
            >
              {t('admin.channels.models-select-none')}
            </Button>
          </div>
          <fieldset
            className="max-h-72 divide-y overflow-y-auto rounded-md border"
            disabled={busy}
            aria-busy={loading}
          >
            <legend className="sr-only">{t('admin.channels.models')}</legend>
            {visible.map((model, index) => (
              <div key={model.modelId} className="flex items-center gap-3 px-3 py-2.5">
                <Checkbox
                  id={`${id}-${index}`}
                  checked={selectedIds.has(model.modelId)}
                  disabled={busy || (atCapacity && !selectedIds.has(model.modelId))}
                  onCheckedChange={(checked) => toggle(model, checked === true)}
                />
                <Label htmlFor={`${id}-${index}`} className="min-w-0 flex-1 cursor-pointer">
                  <span className="flex min-w-0 flex-col gap-1">
                    <span className="truncate">{model.displayName || model.modelId}</span>
                    {model.displayName && model.displayName !== model.modelId && (
                      <span className="text-muted-foreground truncate font-mono text-xs font-normal">
                        {model.modelId}
                      </span>
                    )}
                  </span>
                </Label>
                {model.source === 'manual' && (
                  <Badge variant="outline">{t('admin.channels.models-manual')}</Badge>
                )}
              </div>
            ))}
            {visible.length === 0 && (
              <p className="text-muted-foreground px-3 py-6 text-center text-sm">
                {t('admin.ui.table.no-results')}
              </p>
            )}
          </fieldset>
        </>
      )}

      {models.length === 0 && !error && (
        <p
          className="text-muted-foreground rounded-md border border-dashed px-4 py-6 text-center text-sm"
          role="status"
        >
          {t(loading ? 'admin.channels.models-fetching' : 'admin.channels.models-empty-hint')}
        </p>
      )}

      {atCapacity && (
        <p className="text-muted-foreground text-xs" role="status">
          {t('admin.channels.models-limit', { max: MAX_MODELS })}
        </p>
      )}

      <div className="flex items-center gap-2">
        <Input
          value={manualId}
          onChange={(event) => setManualId(event.target.value)}
          disabled={busy || atCapacity}
          maxLength={200}
          onKeyDown={(event) => {
            if (event.key === 'Enter') {
              event.preventDefault()
              addManual()
            }
          }}
          placeholder={t('admin.channels.models-add-placeholder')}
          aria-label={t('admin.channels.models-add')}
          autoComplete="off"
          spellCheck={false}
        />
        <Button
          type="button"
          variant="outline"
          size="sm"
          disabled={busy || atCapacity || !typed || models.some((model) => model.modelId === typed)}
          onClick={addManual}
        >
          <Plus aria-hidden="true" />
          {t('admin.channels.models-add')}
        </Button>
      </div>
    </PanelSection>
  )
}
