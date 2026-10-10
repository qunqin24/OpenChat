import { ArrowLeft } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { ModelIcon } from '@/components/model-icon'
import { Badge } from '@/components/shadcn/ui/badge'
import { Button } from '@/components/shadcn/ui/button'
import {
  Command,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
} from '@/components/shadcn/ui/command'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/shadcn/ui/tabs'
import { cn } from '@/lib/utils'
import type { ChannelType, ChannelTypeInfo } from '../channel-types'

type ChannelProviderPickerProps = {
  types: ChannelTypeInfo[]
  isPending: boolean
  /** The vendor already chosen. Absent until the operator has chosen one. */
  current?: ChannelType
  onSelect: (type: ChannelType) => void
  /** Present only when there is a form to return to without choosing. */
  onBack?: () => void
}

type Tab = 'all' | ChannelTypeInfo['group']

// Keys are written out rather than built from the value so the typed `t()`
// can check each one against the locale files.
const TABS = [
  { value: 'all', label: 'admin.channels.picker-tab-all' },
  { value: 'vendor', label: 'admin.channels.picker-tab-vendor' },
  { value: 'platform', label: 'admin.channels.picker-tab-platform' },
  { value: 'custom', label: 'admin.channels.picker-tab-custom' },
] as const satisfies readonly { value: Tab; label: string }[]

/**
 * The host of a base URL, or empty when it is not one. It is the part of an
 * endpoint an operator recognises a vendor by, and the whole URL would not fit
 * on a card anyway.
 */
function hostOf(endpoint: string): string {
  try {
    return new URL(endpoint).host
  } catch {
    return ''
  }
}

/**
 * Where adding a channel starts: a searchable, tabbed grid of vendors.
 *
 * It replaces a dropdown because a dropdown of thirty names is a list to read
 * top to bottom, while an operator arrives knowing a name — or, for a relay,
 * only the address. A search over the label, the id and the endpoint serves
 * both, and the tabs narrow the rest by kind of thing.
 *
 * It is built on cmdk for the keyboard, not the filtering. cmdk's own fuzzy
 * match ranks by the item's `value`, which here is an id the operator never
 * sees; filtering by hand over the three strings that are meaningful (and
 * switching cmdk's off) keeps what matches in step with what is drawn.
 *
 * The protocol is deliberately absent from the cards, for the reason the form
 * has no protocol field: the vendor decides it, and showing both invites the
 * reading that they can be mixed.
 */
export function ChannelProviderPicker({
  types,
  isPending,
  current,
  onSelect,
  onBack,
}: ChannelProviderPickerProps) {
  const { t } = useTranslation()
  const [search, setSearch] = useState('')
  const [tab, setTab] = useState<Tab>('all')

  const query = search.trim().toLowerCase()
  const visible = types.filter(
    (type) =>
      (tab === 'all' || type.group === tab) &&
      (query === '' ||
        `${type.label} ${type.id} ${type.endpoint}`.toLowerCase().includes(query)),
  )

  return (
    <div className="flex min-h-0 flex-1 flex-col gap-3 px-4 py-1">
      {onBack && (
        <Button
          type="button"
          variant="ghost"
          size="sm"
          className="-ms-2 self-start"
          onClick={onBack}
        >
          <ArrowLeft aria-hidden="true" />
          {t('admin.channels.picker-back')}
        </Button>
      )}

      {/* cmdk's own filter is off: see the note on the component. The search
          box wears a full border so it reads as the field it is, rather than
          the underlined row a command palette draws. */}
      <Command
        shouldFilter={false}
        label={t('admin.channels.picker-title')}
        className="h-auto min-h-0 flex-1 gap-3 bg-transparent **:data-[slot=command-input-wrapper]:rounded-md **:data-[slot=command-input-wrapper]:border"
      >
        <CommandInput
          // The first thing to do here is type, and the sheet would otherwise
          // hand focus to whatever is tabbable first.
          autoFocus
          value={search}
          onValueChange={setSearch}
          placeholder={t('admin.channels.picker-search')}
        />

        <Tabs
          value={tab}
          onValueChange={(next) => setTab(next as Tab)}
          className="min-h-0 flex-1"
        >
          {/* The tab strip lives inside the command root, so its keys bubble to
              cmdk, where Enter would pick whichever card is highlighted. A tab
              trigger has no use for Enter — radix selects on focus — so it is
              stopped here, after the trigger has seen it. */}
          <TabsList
            className="w-full"
            onKeyDown={(event) => {
              if (event.key === 'Enter') event.stopPropagation()
            }}
          >
            {TABS.map(({ value, label }) => (
              <TabsTrigger key={value} value={value} className="px-1.5 text-xs sm:text-sm">
                {t(label)}
              </TabsTrigger>
            ))}
          </TabsList>

          {/* The scroll container is this, not the list: cmdk's list is sized to
              its content, and it is the tab panel that has a bounded height to
              scroll within. */}
          <TabsContent value={tab} className="min-h-0 overflow-y-auto">
            <CommandList className="max-h-none overflow-visible">
              {isPending ? (
                <p className="text-muted-foreground py-6 text-center text-sm">
                  {t('state.loading')}
                </p>
              ) : (
                <>
                  <CommandEmpty className="text-muted-foreground py-6 text-center text-sm">
                    {t('admin.channels.picker-empty')}
                  </CommandEmpty>
                  <CommandGroup className="p-1 **:[[cmdk-group-items]]:grid **:[[cmdk-group-items]]:grid-cols-1 **:[[cmdk-group-items]]:gap-2 min-[420px]:**:[[cmdk-group-items]]:grid-cols-2">
                    {visible.map((type) => {
                      const host = hostOf(type.endpoint)
                      const isCurrent = type.id === current
                      return (
                        <CommandItem
                          key={type.id}
                          value={type.id}
                          onSelect={() => onSelect(type.id)}
                          className={cn(
                            'items-center gap-3 rounded-lg border p-3',
                            isCurrent && 'border-primary',
                          )}
                        >
                          {/* A brand, not a models.dev provider id. Empty for
                              the relay entry, which has no brand to draw;
                              ModelIcon falls back to a neutral square. */}
                          <ModelIcon brand={type.iconId} name={type.label} className="size-8" />
                          <span className="min-w-0 flex-1">
                            <span className="flex items-center gap-2">
                              <span className="truncate text-sm font-medium">{type.label}</span>
                              {isCurrent && (
                                <Badge variant="secondary" className="shrink-0">
                                  {t('admin.channels.picker-current')}
                                </Badge>
                              )}
                            </span>
                            <span className="text-muted-foreground block truncate text-xs">
                              {host || t('admin.channels.picker-relay')}
                            </span>
                          </span>
                        </CommandItem>
                      )
                    })}
                  </CommandGroup>
                </>
              )}
            </CommandList>
          </TabsContent>
        </Tabs>
      </Command>
    </div>
  )
}
